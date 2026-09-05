/**
 * Snippets: the arithmetic behind the sheet, and the two promises it makes.
 *
 * The two promises are that a press **inserts and never sends**, and that a control whose route
 * this transport does not have is **not drawn at all**. Neither is checkable by opening the page,
 * because both are about what does *not* happen; so the grouping, the order, the join rule and
 * the guard are pure functions here, and the sheet's own module is read as text for the two
 * facts a pure function cannot hold — which function it calls, and where its menu row goes.
 *
 * **Nothing in this file stubs a global.** `view/snippets-data.js` and `core/compose-text.js` are
 * imported into a bare Node process on purpose: `core/pages.js` records why a module that reaches
 * `document` while it is being evaluated is a module no suite can import, and the only way that
 * stays true of these two is if something imports them with no document to reach.
 */
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

assert.equal(typeof globalThis.document, "undefined",
    "this suite stubs no document — the modules below are imported without one");
assert.equal(typeof globalThis.window, "undefined", "and without a window");

const data = await import("../Resources/web/app/js/view/snippets-data.js");
const { snippetActions, snippetControls, snippetCreateBody, snippetDraft, snippetDraftFromText,
    snippetDraftProblem, snippetGroups, snippetOrder, snippetOrderBody, snippetPatchBody,
    snippetReorder, snippetScopeSwap, snippetStarters, snippetSummary, snippetTitle,
    snippetsListHTML } = data;
const { generatedMark, markForSession, projectLabel } =
    await import("../Resources/web/app/js/view/project-mark.js");
const { appendGap, appendedText } = await import("../Resources/web/app/js/core/compose-text.js");
const { T } = await import("../Resources/web/app/js/core/i18n.js");

const sheetSource = await readFile(
    new URL("../Resources/web/app/js/input/snippets.js", import.meta.url), "utf8");
const composerSource = await readFile(
    new URL("../Resources/web/app/js/input/composer.js", import.meta.url), "utf8");
const liveSource = await readFile(
    new URL("../Resources/web/app/js/net/live.js", import.meta.url), "utf8");
const clientSource = await readFile(
    new URL("../Resources/web/app/js/net/client.js", import.meta.url), "utf8");
const cloudSource = await readFile(
    new URL("../Resources/web/app/js/net/cloud-client.js", import.meta.url), "utf8");
const styles = await readFile(
    new URL("../Resources/web/app/css/snippets.css", import.meta.url), "utf8");
const headSource = await readFile(
    new URL("../Resources/web/app/js/view/transcript.js", import.meta.url), "utf8");
const headStyles = await readFile(
    new URL("../Resources/web/app/css/detail.css", import.meta.url), "utf8");
const indexSource = await readFile(
    new URL("../Resources/web/index.html", import.meta.url), "utf8");
const mockSource = await readFile(
    new URL("../Resources/web/app/js/net/mock.js", import.meta.url), "utf8");

/* ---- the words -----------------------------------------------------------
   The `⋯` menu is a shipped surface and every other row in it comes from the string table, so
   these five are read from `T` rather than written into the view. */
for (const key of ["webSnippets", "webSnippetsThisProject", "webSnippetsEveryProject",
    "webSnippetsEmpty", "webSnippetsEmptyNew", "webSnippetsReadOnly",
    "webSnippetNew", "webSnippetEditing", "webSnippetMore", "webSnippetEdit", "webSnippetDelete",
    "webSnippetDeleteAsk", "webSnippetUp", "webSnippetDown", "webSnippetToGlobal",
    "webSnippetToProject", "webSnippetTitleLabel", "webSnippetBodyLabel", "webSnippetScopeLabel",
    "webSnippetSave", "webSnippetFromLast", "webSnippetNeedsText",
    "webSnippetStarterCommitTitle", "webSnippetStarterCommitBody",
    "webSnippetStarterReportTitle", "webSnippetStarterReportBody"]) {
    assert.equal(typeof T[key], "string", key + " has an English fallback in core/i18n.js");
    assert.ok(T[key].length > 0, key + " is not the empty string");
}

/* ---- which transport may do what ------------------------------------------ */

// The direct path, as `net/live.js` presents it.
const direct = {
    snippets() { return Promise.resolve({}); },
    createSnippet() { return Promise.resolve({}); },
    updateSnippet() { return Promise.resolve({}); },
    deleteSnippet() { return Promise.resolve({}); },
    orderSnippets() { return Promise.resolve({}); }
};
// The relay, which reads out of the published snapshot and cannot write config at all.
const relay = { snippets() { return Promise.resolve({}); } };
// The client the cloud page runs before a relay has been chosen: no snippets route of any kind.
const idle = { schedules() { return Promise.resolve({ schedules: [] }); } };

assert.deepEqual(snippetControls(direct),
    { read: true, create: true, update: true, remove: true, order: true },
    "the direct path has all five routes");
assert.deepEqual(snippetControls(relay),
    { read: true, create: false, update: false, remove: false, order: false },
    "the relay reads and cannot write");
assert.deepEqual(snippetControls(idle),
    { read: false, create: false, update: false, remove: false, order: false },
    "a transport with no snippets route at all answers false to every question");
assert.deepEqual(snippetControls(null),
    { read: false, create: false, update: false, remove: false, order: false },
    "and so does no transport");
assert.equal(snippetControls({ snippets: "yes" }).read, false,
    "a property that is not a function is not a route");

// The guard is `typeof … === "function"` on the transport, which is the pattern `/v1/places` and
// `/v1/push/key` already use, and it is asked in the sheet rather than assumed.
assert.match(sheetSource, /snippetControls\(api\)/,
    "the sheet asks the live transport what it can do");
assert.match(sheetSource, /var ok = readable\(\);\n    button\.hidden = !ok;\n    button\.disabled = !ok;/,
    "and leaves its own menu row undrawn when the transport cannot even read the list — "
    + "disabled as well as hidden, because SessionActions.items() collects "
    + "button:not(:disabled) for the arrow keys");
assert.match(sheetSource,
    /getElementById\("detail-actions-trigger"\)\.addEventListener\("click", syncRow\)/,
    "and asks again every time the menu opens: the transport is chosen after these modules "
    + "evaluate, and renderTranscript emits no clawdline:rendered for a transcript it could "
    + "not read");

// The frozen contract stays five methods short of this feature: the relay satisfies it and has
// none of the writing half, so adding any of them would make a transport that works illegal.
const methods = /methods:\s*Object\.freeze\(\[([^\]]*)\]\)/.exec(clientSource)[1];
for (const name of ["snippets", "createSnippet", "updateSnippet", "deleteSnippet",
    "orderSnippets"]) {
    assert.ok(!methods.includes('"' + name + '"'),
        name + "() is not on ClawdlineClient.methods");
}
for (const name of ["snippets", "createSnippet", "updateSnippet", "deleteSnippet",
    "orderSnippets"]) {
    assert.ok(liveSource.includes("LocalClient." + name + " = function"),
        "the direct transport carries " + name + "()");
}
assert.match(cloudSource, /\n    snippets\(\) \{/, "the cloud transport carries snippets()");
for (const name of ["createSnippet", "updateSnippet", "deleteSnippet", "orderSnippets"]) {
    assert.ok(!cloudSource.includes(name + "("),
        "the cloud transport does not carry " + name + "() — absent, not rejecting");
}

/* ---- grouping and ordering ------------------------------------------------ */

const project = "/Users/you/code/clawdline";
const answer = {
    project: { key: project, label: "clawdline" },
    snippets: [
        { id: "g-late", title: "Deploy", body: "commit, push, deploy", scope: "global",
          position: 300 },
        { id: "p-only", title: "Focused", body: "run the focused groups", scope: "project",
          project: project, position: 200 },
        { id: "g-early", title: "Recap", body: "say what you did", scope: "global",
          position: 100 }
    ]
};

const grouped = snippetGroups(answer, {});
assert.deepEqual(grouped.project, { key: project, label: "clawdline" },
    "the resolved scope comes back from the Mac's own answer");
assert.deepEqual(grouped.groups.map((group) => group.scope), ["project", "global"],
    "this project is the first group and every project is the second");
assert.deepEqual(grouped.groups[0].rows.map((row) => row.id), ["p-only"]);
assert.deepEqual(grouped.groups[1].rows.map((row) => row.id), ["g-early", "g-late"],
    "each group is in the order the person put them in, not the order they arrived in");
assert.equal(grouped.count, 3);
assert.deepEqual(snippetOrder(grouped).map((row) => row.id), ["p-only", "g-early", "g-late"],
    "a press index counts down the sheet, project group first");

const unpositioned = snippetGroups({
    project: { key: project, label: "clawdline" },
    snippets: [
        { id: "none-a", body: "a", scope: "global" },
        { id: "third", body: "c", scope: "global", position: 900 },
        { id: "none-b", body: "b", scope: "global" }
    ]
}, {});
assert.deepEqual(unpositioned.groups[1].rows.map((row) => row.id), ["third", "none-a", "none-b"],
    "a record written before positions existed goes last, keeping the order it arrived in");

assert.deepEqual(snippetGroups({}, {}).groups.map((group) => group.rows.length), [0, 0],
    "an answer with no list at all is two empty groups rather than a throw");
assert.equal(snippetGroups(null, {}).project, null,
    "and no project, rather than a project called undefined");

const noisy = snippetGroups({
    project: { key: project, label: "clawdline" },
    snippets: [null, "text", { id: "empty", body: "", scope: "global" },
        { id: "odd", body: "kept out", scope: "elsewhere" },
        { id: "kept", body: "kept", scope: "global", position: 1 }]
}, {});
assert.deepEqual(snippetOrder(noisy).map((row) => row.id), ["kept"],
    "a row with no body, an unknown scope, or no object at all is not a row");

/* The relay's answer: whole records, no resolved project, tagged with the machine that published
   them. The project group is matched by equality against the session's own cwd and by nothing
   else — the registry prefix and the worktree fold are the Mac's rule and stay there. */
const published = {
    snippets: [
        { id: "here", body: "mine", scope: "project", project: project, position: 1,
          machine: "mac-01" },
        { id: "elsewhere", body: "another project", scope: "project",
          project: "/Users/you/code/atrium", position: 2, machine: "mac-01" },
        { id: "other-mac", body: "not this Mac", scope: "project", project: project,
          position: 3, machine: "mac-02" },
        { id: "everywhere", body: "global", scope: "global", position: 4, machine: "mac-01" },
        { id: "other-mac-global", body: "global elsewhere", scope: "global", position: 5,
          machine: "mac-02" }
    ]
};
const onRelay = snippetGroups(published, { machine: "mac-01", project: project });
assert.deepEqual(snippetOrder(onRelay).map((row) => row.id), ["here", "everywhere"],
    "another project's rows and another Mac's rows are both left out");
assert.deepEqual(onRelay.project, { key: project, label: "clawdline" },
    "with no label published, the project is named by the tail of its own path");

const subdirectory = snippetGroups(published,
    { machine: "mac-01", project: project + "/docs" });
assert.deepEqual(snippetOrder(subdirectory).map((row) => row.id), ["everywhere"],
    "a session in a subdirectory sees the global group on the relay: a smaller answer, "
    + "because the browser does not resolve scope keys");

assert.deepEqual(
    snippetOrder(snippetGroups(published, { machine: "mac-01" })).map((row) => row.id),
    ["everywhere"],
    "and with no project known at all, no project row can be claimed by this session");

/* ---- what a row says ------------------------------------------------------ */

assert.equal(snippetSummary("  first line  \nsecond line"), "first line",
    "the row shows the first line of the body and nothing under it");
assert.equal(snippetSummary("\n\n  after the blanks\nmore"), "after the blanks",
    "leading blank lines are not the first line");
assert.equal(snippetSummary(""), "");
assert.equal(snippetSummary(null), "");
assert.equal(snippetSummary("x".repeat(400)).length, 140,
    "a four-thousand-character body does not become a four-thousand-character row");
assert.ok(snippetSummary("x".repeat(400)).endsWith("…"));
assert.equal(snippetTitle({ title: "  Deploy  ", body: "b" }), "Deploy");
assert.equal(snippetTitle({ title: "   ", body: "commit, push, deploy" }), "commit, push, deploy",
    "a record whose title never made it still says what it will insert");

/* ---- the join rule, which is the composer's and is now written down once --- */

assert.equal(appendGap(""), "", "nothing goes in front of the first word");
assert.equal(appendGap("   "), "", "and nothing in front of the whitespace a browser left behind");
assert.equal(appendGap("already there"), " ",
    "a snippet pressed after something typed is a second sentence, not a longer word");
assert.equal(appendGap("ends with a space "), "",
    "a space the person typed is not doubled");
assert.equal(appendGap("ends with a line\n"), "", "and neither is a newline");
assert.equal(appendedText("先跑測試", "commit、push、deploy。"), "先跑測試 commit、push、deploy。");
assert.equal(appendedText("", "commit、push、deploy。"), "commit、push、deploy。");
assert.equal(appendedText(null, null), "");

// One definition, and `appendMsg` is the thing that uses it. A second insertion path that
// re-derived this rule is exactly how the two halves drift.
assert.match(composerSource, /import \{ appendGap, appendedText \} from "\.\.\/core\/compose-text\.js"/,
    "the composer takes the join rule from the module this suite tested");
assert.match(composerSource, /insertText\(appendGap\(had\) \+ said\)/,
    "the caret path joins with it");
assert.match(composerSource, /els\.msg\.textContent = appendedText\(had, said\)/,
    "and so does the path that rewrites the box");

/* ---- what the sheet draws ------------------------------------------------- */

const drawn = snippetsListHTML(grouped, { controls: snippetControls(direct) });
assert.ok(drawn.includes(T.webSnippetsThisProject), "the project group carries its heading");
assert.ok(drawn.includes(T.webSnippetsEveryProject), "and so does the global group");
assert.ok(drawn.includes(">clawdline</span>"), "the heading names the project it means");
assert.equal((drawn.match(/class="snippet-row"/g) || []).length, 3, "one button per snippet");
assert.deepEqual(drawn.match(/data-snippet="\d+"/g), ['data-snippet="0"', 'data-snippet="1"',
    'data-snippet="2"'], "the press index is the position in snippetOrder()");
assert.ok(!/data-snippet="\d+" disabled/.test(drawn),
    "a device that may write draws no disabled row");
assert.equal((drawn.match(/data-snippet-more="\d+"/g) || []).length, 3,
    "and every row on a transport that can write carries its own menu button");

// **The guard, which is the whole reason this feature has two halves.** The relay reads the
// list out of the published snapshot and has no envelope class for a write, so it draws the
// same rows and not one control that would fail when pressed. Absent, not disabled: a dead
// button is worse than no button, and `assert.equal` on the two strings is what stops a later
// hand from "just disabling" one of them.
const onRelayHTML = snippetsListHTML(grouped, { controls: snippetControls(relay) });
assert.equal(onRelayHTML.replace(/<button class="snippet-more"[^]*?<\/button>/g, ""),
    onRelayHTML, "a relay reader is drawn no row menu at all");
for (const marker of ["data-snippet-edit", "data-snippet-delete", "data-snippet-scope",
    "data-snippet-up", "data-snippet-down", "data-snippet-more", "snippet-menu"]) {
    assert.ok(!onRelayHTML.includes(marker),
        "no writing control reaches a transport that cannot write: " + marker);
}
assert.equal(onRelayHTML,
    snippetsListHTML(grouped, { controls: snippetControls(relay), menuFor: 1 }),
    "and asking for a menu on a relay row changes nothing, because there is no menu to open");

const readOnly = snippetsListHTML(grouped, { controls: snippetControls(direct), readOnly: true });
assert.equal((readOnly.match(/class="snippet-row" type="button" data-snippet="\d+" disabled/g)
    || []).length, 3, "a device that may read but not write sees every row, and cannot press one");
assert.ok(readOnly.includes(T.webSnippetsReadOnly), "with the reason on screen");
for (const marker of ["data-snippet-edit", "data-snippet-delete", "data-snippet-more"]) {
    assert.ok(!readOnly.includes(marker),
        "and S.write === false draws no writing control either: " + marker);
}

assert.equal(snippetsListHTML(grouped, { loading: true }), "",
    "before the answer arrives the list is empty rather than claiming there are none");
assert.ok(snippetsListHTML(snippetGroups({ snippets: [] }, {}), {})
    .includes(T.webSnippetsEmpty), "an answer with no rows is the empty state");
assert.ok(snippetsListHTML(grouped, { error: "this Mac does not publish its snippets" })
    .includes("this Mac does not publish its snippets"),
    "a transport's refusal is shown in the transport's own words");

const dangerous = snippetGroups({
    project: { key: project, label: "<b>clawdline</b>" },
    snippets: [{ id: "x", title: "<img src=x onerror=alert(1)>",
        body: "<script>alert(1)</script>", scope: "global", position: 1 }]
}, {});
const escaped = snippetsListHTML(dangerous, {});
assert.ok(!escaped.includes("<script>"), "a body is text, not markup");
assert.ok(!escaped.includes("<img src=x"), "and so is a title");
assert.ok(!escaped.includes("<b>clawdline</b>"), "and so is a project label");

/* ---- the two facts a pure function cannot hold ---------------------------- */

assert.match(sheetSource, /import \{ appendMsg \} from "\.\/composer\.js"/,
    "the sheet inserts through the one function dictation already uses");
assert.match(sheetSource, /closeSnippets\(\);\n    appendMsg\(row\.body\);/,
    "a press closes the sheet and puts the body in the box");
for (const forbidden of [/api\.send\(/, /\.send\(/, /Idempotency/]) {
    assert.ok(!forbidden.test(sheetSource),
        "nothing in this sheet sends: " + forbidden);
}
assert.match(sheetSource, /button\.id = "session-snippets"/, "the menu row has its own id");
assert.match(sheetSource, /getElementById\("session-git-more"\)[\s\S]*insertBefore\(button, before\)/,
    "and sits in #session-actions-main before the Git row");
assert.match(sheetSource, /export function openSnippets\(\)/,
    "the sheet opens from a plain function call, so the header can reuse it");

assert.match(styles, /\.snippet-list\s*\{[^}]*overflow-x:\s*hidden;/s,
    "the list scroller never acquires a horizontal axis");
assert.match(styles, /\.snippet-row-line\s*\{[^}]*text-overflow:\s*ellipsis;/s,
    "a long first line is clipped rather than laying out the sheet");
assert.match(styles, /\.snippet-row\[disabled\]/,
    "the read-only row has a look of its own");


/* ==========================================================================
   The editor
   ========================================================================== */

/* ---- which controls exist, and where they stop existing ------------------- */

assert.deepEqual(snippetActions(snippetControls(direct), false),
    { create: true, update: true, remove: true, order: true, menu: true },
    "a device that may write, on a transport that has the routes, gets all of it");
assert.deepEqual(snippetActions(snippetControls(relay), false),
    { create: false, update: false, remove: false, order: false, menu: false },
    "the relay reads and cannot write, so it gets none of it");
assert.deepEqual(snippetActions(snippetControls(direct), true),
    { create: false, update: false, remove: false, order: false, menu: false },
    "and a read-only device gets none of it either, on the same transport that has every route");
assert.equal(snippetActions({ update: true }, false).menu, true,
    "the row menu exists when any one of its items would");
assert.equal(snippetActions({ create: true }, false).menu, false,
    "and not when its only route is the one that makes a new snippet — an empty menu is a "
    + "button that opens nothing");
assert.deepEqual(snippetActions(null, false),
    { create: false, update: false, remove: false, order: false, menu: false },
    "no transport is no control");

/* ---- what the editor starts with ----------------------------------------- */

const blank = snippetDraft(null, {});
assert.equal(blank.scope, "global",
    "a new snippet defaults to every project — that is the common case and it was the ask");
assert.deepEqual([blank.id, blank.title, blank.body, blank.project], ["", "", "", ""]);

const editingProject = snippetDraft(
    { id: "p-only", title: "Focused", body: "run them", scope: "project", project }, {});
assert.deepEqual(editingProject,
    { id: "p-only", title: "Focused", body: "run them", scope: "project", project: project },
    "a row being changed keeps everything it had, scope included");
assert.equal(snippetDraft({ id: "x", body: "b", scope: "nonsense" }, {}).scope, "global",
    "a scope nobody recognises is not carried into the editor");

/* ---- what it refuses to save --------------------------------------------- */

assert.equal(snippetDraftProblem({ title: "t", body: "b", scope: "global" }), "");
assert.equal(snippetDraftProblem({ title: "  ", body: "b", scope: "global" }), "empty",
    "a title of spaces is not a title");
assert.equal(snippetDraftProblem({ title: "t", body: "\n \n", scope: "global" }), "empty",
    "and a body of blank lines is not a body");
assert.equal(snippetDraftProblem({ title: "x".repeat(61), body: "b", scope: "global" }), "long",
    "sixty characters is the store's limit, counted here so the Mac never has to refuse it");
assert.equal(snippetDraftProblem({ title: "x".repeat(60), body: "b", scope: "global" }), "",
    "and exactly sixty is inside it");
assert.equal(snippetDraftProblem({ title: "t", body: "b".repeat(4001), scope: "global" }), "long");
assert.equal(snippetDraftProblem({ title: "t", body: "b", scope: "project", project: "" }),
    "scope", "a project scope with no project is the store's snippet_scope_mismatch");
assert.equal(snippetDraftProblem(null), "empty", "and nothing at all is not saveable either");

/* ---- the bodies, whose key sets are exact --------------------------------- */

assert.deepEqual(snippetCreateBody({ title: " Deploy ", body: " commit, push ", scope: "global" }),
    { title: "Deploy", body: "commit, push", scope: "global" },
    "a global snippet carries no project key at all — not even a null one, which the store "
    + "counts as present and refuses");
assert.ok(!Object.prototype.hasOwnProperty.call(
    snippetCreateBody({ title: "t", body: "b", scope: "global" }), "project"),
    "said again as a key check, because `project: null` is the mistake this prevents");
assert.deepEqual(snippetCreateBody({ title: "t", body: "b", scope: "project", project }),
    { title: "t", body: "b", scope: "project", project: project },
    "and a project snippet carries exactly one");
assert.equal(snippetCreateBody({ title: "", body: "b", scope: "global" }), null,
    "a draft that is not ready cannot be sent by accident");

const stored = { id: "g-late", title: "Deploy", body: "commit, push, deploy", scope: "global",
    position: 300 };
assert.deepEqual(snippetPatchBody(snippetDraft(stored, {}), stored), {},
    "changing nothing is a sheet to close, not a patch to send — the store refuses an empty one");
assert.deepEqual(
    snippetPatchBody({ title: "Ship it", body: stored.body, scope: "global" }, stored),
    { title: "Ship it" }, "one field changed is one field sent");
assert.deepEqual(
    snippetPatchBody({ title: stored.title, body: "just push", scope: "global" }, stored),
    { body: "just push" });
assert.deepEqual(
    snippetPatchBody({ title: stored.title, body: stored.body, scope: "project", project },
        stored),
    { scope: "project", project: project },
    "scope and project move together or the store refuses the pair");
const storedLocal = { id: "p", title: "Focused", body: "run them", scope: "project", project };
assert.deepEqual(
    snippetPatchBody({ title: "Focused", body: "run them", scope: "global" }, storedLocal),
    { scope: "global" },
    "and going the other way sends no project at all, which is how the store clears it");
assert.equal(snippetPatchBody({ title: "", body: "b", scope: "global" }, stored), null);

/* ---- the row menu's scope item ------------------------------------------- */

assert.deepEqual(snippetScopeSwap(storedLocal, project), { scope: "global" },
    "a project row moves to every project");
assert.deepEqual(snippetScopeSwap(stored, project), { scope: "project", project: project },
    "and a global row moves into the project the Mac resolved");
assert.equal(snippetScopeSwap(stored, ""), null,
    "a session whose project the Mac did not resolve has nowhere to move it, so the item is "
    + "not offered");
assert.equal(snippetScopeSwap(null, project), null);
assert.equal(snippetScopeSwap({ scope: "elsewhere" }, project), null);

/* ---- reorder arithmetic --------------------------------------------------- */

const ordered = [{ id: "a" }, { id: "b" }, { id: "c" }];
assert.deepEqual(snippetReorder(ordered, "b", -1).map((row) => row.id), ["b", "a", "c"],
    "up swaps a row with the one above it");
assert.deepEqual(snippetReorder(ordered, "b", 1).map((row) => row.id), ["a", "c", "b"],
    "and down with the one below");
assert.deepEqual(ordered.map((row) => row.id), ["a", "b", "c"],
    "without moving the array it was given — the drawn list is not the request");
assert.equal(snippetReorder(ordered, "a", -1), null, "the first row cannot go up");
assert.equal(snippetReorder(ordered, "c", 1), null, "and the last cannot go down");
assert.equal(snippetReorder(ordered, "nobody", -1), null);
assert.equal(snippetReorder([], "a", 1), null);
assert.deepEqual(snippetReorder(ordered, "c", -1).map((row) => row.id), ["a", "c", "b"],
    "moving c up and moving b down are the same swap, said twice");

assert.deepEqual(snippetOrderBody("global", "", ordered),
    { scope: "global", order: ["a", "b", "c"] },
    "the global order carries no project key — the store compares the key set exactly");
assert.deepEqual(snippetOrderBody("project", project, ordered),
    { scope: "project", project: project, order: ["a", "b", "c"] });
assert.equal(snippetOrderBody("project", "", ordered), null,
    "and a project order with no project is refused here rather than by the Mac");
assert.equal(snippetOrderBody("global", "", []), null, "an empty scope has no order to send");
assert.equal(snippetOrderBody("global", "", [{ id: "a" }, {}]), null,
    "a row with no id makes the whole order null rather than one the store refuses halfway");
assert.equal(snippetOrderBody("elsewhere", "", ordered), null);

/* ---- the starters, and my last message ------------------------------------ */

const starters = snippetStarters();
assert.equal(starters.length, 2, "the two this design was written from");
assert.deepEqual(starters.map((one) => one.scope), ["global", "global"],
    "both global: a starter that belonged to one project would teach the wrong thing about "
    + "the feature on its first use");
assert.equal(starters[0].title, T.webSnippetStarterCommitTitle);
assert.equal(starters[0].body, T.webSnippetStarterCommitBody);
assert.equal(starters[1].title, T.webSnippetStarterReportTitle);
assert.equal(starters[1].body, T.webSnippetStarterReportBody);
for (const one of starters) {
    assert.equal(snippetDraftProblem(snippetDraft(null, one)), "",
        one.key + " is savable exactly as it arrives — a starter that needed editing first "
        + "would not be a starter");
}

const fromMessage = snippetDraftFromText(
    "  跑一次 focused groups\n先看到紅的再看到綠的。  ");
assert.equal(fromMessage.body, "跑一次 focused groups\n先看到紅的再看到綠的。",
    "the whole message is the body, with only the whitespace at its ends taken off");
assert.equal(fromMessage.title, "跑一次 focused groups", "and its first line is the title");
assert.equal(fromMessage.scope, "global");
const longMessage = snippetDraftFromText("x".repeat(200));
assert.equal(longMessage.title.length, 60,
    "a title made from a long first line is cut to the store's sixty");
assert.ok(longMessage.title.endsWith("…"), "and says that it was cut");
assert.equal(snippetDraftProblem(longMessage), "",
    "which means the draft it produces is one the store will take");
assert.equal(snippetDraftFromText("").title, "");
assert.equal(snippetDraftFromText(null).body, "");

/* ---- what the empty state offers ------------------------------------------ */

const emptyModel = snippetGroups({ project: { key: project, label: "clawdline" }, snippets: [] },
    {});
const emptyWritable = snippetsListHTML(emptyModel, { controls: snippetControls(direct) });
assert.ok(emptyWritable.includes(T.webSnippetsEmptyNew),
    "a device that can write is shown the door rather than told to go to the Mac");
assert.equal((emptyWritable.match(/data-snippet-starter="\d+"/g) || []).length, 2,
    "and the two starters, one press each");
assert.ok(emptyWritable.includes(T.webSnippetStarterCommitTitle));
assert.ok(emptyWritable.includes(T.webSnippetStarterReportTitle));

const emptyRelay = snippetsListHTML(emptyModel, { controls: snippetControls(relay) });
assert.ok(emptyRelay.includes(T.webSnippetsEmpty),
    "a relay reader is told where snippets come from, which is the only useful thing to say "
    + "to somebody who cannot make one here");
assert.ok(!emptyRelay.includes("data-snippet-starter"),
    "and offered no starter, because pressing one would open an editor that cannot save");
assert.ok(!snippetsListHTML(emptyModel,
    { controls: snippetControls(direct), readOnly: true }).includes("data-snippet-starter"),
    "nor is a read-only device");

/* ---- the row menu --------------------------------------------------------- */

const withMenu = snippetsListHTML(grouped, { controls: snippetControls(direct), menuFor: 1 });
assert.equal((withMenu.match(/class="snippet-menu"/g) || []).length, 1,
    "one menu is open at a time, under the row it belongs to");
assert.ok(withMenu.includes('data-snippet-edit="1"'), "it edits that row");
assert.ok(withMenu.includes('data-snippet-delete="1"'));
assert.ok(withMenu.includes('data-snippet-scope="1"'));
assert.ok(withMenu.includes(T.webSnippetToProject),
    "a global row's scope item offers the project, in the project's own words");
assert.ok(!withMenu.includes('data-snippet-up="1"'),
    "the first row of the global group cannot go up — the item is absent, not disabled");
assert.ok(withMenu.includes('data-snippet-down="1"'), "and it can go down");
assert.ok(withMenu.includes('aria-expanded="true"'), "the row's own button says it is open");

const lastMenu = snippetsListHTML(grouped, { controls: snippetControls(direct), menuFor: 2 });
assert.ok(lastMenu.includes('data-snippet-up="2"'));
assert.ok(!lastMenu.includes('data-snippet-down="2"'), "and the last row cannot go down");

const oneRowMenu = snippetsListHTML(grouped, { controls: snippetControls(direct), menuFor: 0 });
assert.ok(!oneRowMenu.includes("data-snippet-up") && !oneRowMenu.includes("data-snippet-down"),
    "a group of one has nowhere to move, so neither arrow is drawn — the ordering is per "
    + "scope, and a row's neighbours are the rows in its own group");
assert.ok(oneRowMenu.includes(T.webSnippetToGlobal),
    "and the project row's scope item offers every project");

const orderOnly = snippetsListHTML(grouped,
    { controls: { read: true, order: true }, menuFor: 1 });
assert.ok(!orderOnly.includes("data-snippet-edit") && !orderOnly.includes("data-snippet-delete"),
    "a transport with only the ordering route draws only the arrows");
assert.ok(orderOnly.includes("data-snippet-down"));

/* ---- a project with no mark of its own ------------------------------------ */

const madeUp = generatedMark("/Users/x/tmp/notes");
assert.equal(madeUp.cells.length, 4, "four rows, the same shape every mark in this app has");
assert.equal(madeUp.cells[0].length, 7, "and seven cells across");
assert.match(madeUp.accent, /^#[0-9a-f]{6}$/, "with a colour `tint()` can read");
assert.equal(madeUp.generated, true, "and it says it was made up rather than registered");
for (const row of madeUp.cells) {
    for (let x = 0; x < 3; x++) {
        assert.equal(row[x], row[6 - x],
            "mirrored about the middle column: sixteen coin flips read as noise, and a "
            + "mirrored sixteen reads as an emblem");
    }
}
assert.deepEqual(generatedMark("/Users/x/tmp/notes"), madeUp,
    "the same path is the same mark, on every machine and every reload");
assert.notDeepEqual(generatedMark("/Users/x/tmp/other"), madeUp,
    "and two projects are two marks — a placeholder repeated everywhere says 'no icon', "
    + "which is not what a project mark is for");
assert.deepEqual(generatedMark("/Users/x/code/p/"), generatedMark("/Users/x/code/p"),
    "a trailing slash is the same project");
assert.equal(generatedMark(""), null,
    "no project is not a project with no mark: there is nothing to stand in for");
assert.equal(generatedMark(null), null);

// Density, which is the whole reason this is generated rather than random: a mark with nothing
// lit is an invisible button, and a mark with everything lit is a rectangle. Both identify
// nothing, which is the failure a drawn placeholder would also have had. The band below was
// measured over these five thousand, not guessed: 6 to 23 of 28.
let thinnest = 99, thickest = 0;
for (let i = 0; i < 5000; i++) {
    const mark = generatedMark("/Users/x/code/project-" + i);
    let lit = 0;
    for (const row of mark.cells) for (const cell of row) if (cell === mark.accent) lit += 1;
    thinnest = Math.min(thinnest, lit);
    thickest = Math.max(thickest, lit);
}
assert.ok(thinnest >= 6, "every generated mark has something on it: thinnest was " + thinnest);
assert.ok(thickest <= 24, "and none of them is a solid block: thickest was " + thickest);

assert.equal(markForSession({ cwd: "/Users/x/tmp/notes", icon: null }).generated, true,
    "a session whose project was never registered gets one made up");
const registered = { accent: "#d97757", cells: [["#d97757"]] };
assert.equal(markForSession({ cwd: "/Users/x/code/clawdline", icon: registered }), registered,
    "and a registered icon always wins — this is what a project has until somebody draws it one");
assert.equal(markForSession({ cwd: "/Users/x", icon: { cells: [] } }).generated, true,
    "an icon record with no cells in it is no icon");
assert.equal(markForSession(null), null);

assert.equal(projectLabel("/Users/x/code/clawdline"), "clawdline");
assert.equal(projectLabel("/Users/x/code/clawdline/"), "clawdline");
assert.equal(projectLabel(""), "", "and an unknown project is named nothing, not 'undefined'");

/* ---- the header: pressing the mark is not pressing Session info ----------- */

assert.match(indexSource, /<button class="detail-mark-go" id="detail-snippets" type="button"/,
    "the mark is a button of its own");
assert.match(indexSource,
    /id="detail-snippets"[^]*?<canvas id="detail-mark"[^]*?<\/button>\s*<button class="detail-session" id="detail-info"/,
    "the canvas is inside it, and #detail-info begins after it closes");
const infoButton = /<button class="detail-session" id="detail-info"[^]*?<\/button>/.exec(
    indexSource)[0];
assert.ok(!infoButton.includes("detail-mark"),
    "so pressing the mark is not pressing Session info — the whole point of the split");
assert.ok(infoButton.includes('id="detail-name"') && infoButton.includes('id="detail-sub"'),
    "and Session info keeps the name and the path, which is what a reader points at when they "
    + "mean 'tell me about this session'");
const markButton = /<button class="detail-mark-go"[^]*?<\/button>/.exec(indexSource)[0];
assert.ok(!/<button/.test(markButton.slice(7)),
    "no button is nested inside another one, which no browser would let a person press");

assert.match(headSource, /els\["detail-snippets"\]\.disabled = /,
    "renderDetailHead owns the new button's disabled state, beside the two it already sets");
assert.match(headSource, /els\["detail-snippets"\]\.setAttribute\("aria-label", snippetsSays\)/,
    "and its label");
assert.match(headSource, /var canSnippet = snippetControls\(api\)\.read;/,
    "asked of the same guard the sheet asks, so the header and the ⋯ row cannot disagree");
assert.match(headSource, /els\["detail-snippets"\]\.hidden = !s;/,
    "no session open is no project: the button goes away rather than leaving an empty box");
assert.match(headSource, /drawIcon\(els\["detail-mark"\], mark, 5\)/,
    "and the mark it draws is markForSession's, so an unregistered project is not a hole");

assert.match(headStyles, /\.detail-mark-go\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px;/s,
    "the 44px target is a floor and not only padding: drawIcon sizes that canvas from the "
    + "icon's own cells, and a narrower mark must not shrink the target");
assert.match(headStyles, /\.detail-mark-go\s*\{[^}]*margin:\s*-12px -5px;/s,
    "with a negative margin that takes the space back, the way .detail-session already does");
assert.match(headStyles, /\.detail-mark-go\[data-mark="none"\]/,
    "and a box for the session that still has nothing to draw");
assert.match(headStyles, /\.detail-mark-go\[hidden\]\s*\{\s*display:\s*none;/,
    "a rule that sets display outranks the hidden attribute — the grid above would keep an "
    + "empty box on screen otherwise");

/* ---- the sheet's own new wiring ------------------------------------------- */

assert.match(sheetSource, /getElementById\("detail-snippets"\)/,
    "the header's button opens this sheet");
assert.equal((sheetSource.match(/openSnippets\(\);/g) || []).length, 2,
    "from the same function the ⋯ row calls — one function, two entrances, not two code paths");
assert.match(sheetSource,
    /function may\(\) \{\n    return snippetActions\(snippetControls\(api\), S\.write !== true\);/,
    "and every control this sheet draws asks one question about the transport and the switch");
assert.match(sheetSource, /newButton\.hidden = !can\.create/,
    "the ＋ is the one writing control outside the list, so it is hidden by hand");
assert.match(sheetSource, /api\.orderSnippets\(body\.scope, body\.project \|\| null, body\.order\)/,
    "reordering sends the full order of one scope");
assert.match(sheetSource, /userMessageEntries\(/,
    "'from my last message' asks the sheet next door rather than walking the transcript again");
assert.ok(!/S\.tx\.entries\.filter/.test(sheetSource),
    "which is a call, not a second copy of that rule");

for (const forbidden of [/createSnippet\s*=/, /uuid\(/]) {
    assert.ok(!forbidden.test(sheetSource),
        "the sheet calls the transport rather than building its own request: " + forbidden);
}

assert.match(styles, /\.snippet-more\s*\{[^}]*min-width:\s*44px;/s,
    "a row's own menu button is a thumb target too");
assert.match(styles, /\.snippets-new\[hidden\]\s*\{\s*display:\s*none;/,
    "and the ＋ really disappears, rather than being a grid rule that outranks its attribute");

/* ---- the fixture writes, so the editor can be seen without a Mac ---------- */

for (const name of ["createSnippet", "updateSnippet", "deleteSnippet", "orderSnippets"]) {
    assert.ok(mockSource.includes("Mock." + name + " = function"),
        "the mock carries " + name + "(), or the editor cannot be looked at with ?mock=1");
}
assert.match(mockSource, /snippets"\) === "readonly"/,
    "and one URL takes the writing half away, which is the Cloud path's shape");
assert.match(mockSource, /snippet_scope_mismatch/,
    "the fixture refuses a project beside a global scope the way the store does — that is the "
    + "shape a body built by copying a row takes, and a fixture that accepted it would teach "
    + "this page a habit Sources/Snippets.swift breaks on the first real Mac");

console.log("web snippet tests passed");
