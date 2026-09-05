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
const { snippetControls, snippetGroups, snippetOrder, snippetSummary, snippetTitle,
    snippetsListHTML } = data;
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

/* ---- the words -----------------------------------------------------------
   The `⋯` menu is a shipped surface and every other row in it comes from the string table, so
   these five are read from `T` rather than written into the view. */
for (const key of ["webSnippets", "webSnippetsThisProject", "webSnippetsEveryProject",
    "webSnippetsEmpty", "webSnippetsReadOnly"]) {
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
assert.ok(!drawn.includes("disabled"), "a device that may write draws no disabled row");

// No control this wave has nothing to open — on either transport. The editor's `＋` and the
// row's own `⋯` arrive with the editor, behind snippetControls; a menu drawn before the thing
// it opens is the dead button the guard exists to prevent.
const onRelayHTML = snippetsListHTML(grouped, { controls: snippetControls(relay) });
assert.equal(onRelayHTML, drawn,
    "the list a relay reader sees is the list a direct reader sees, because neither carries a "
    + "writing control yet");
for (const html of [drawn, onRelayHTML]) {
    for (const marker of ["data-snippet-edit", "data-snippet-delete", "snippet-new",
        "snippet-more"]) {
        assert.ok(!html.includes(marker), "no writing control is drawn: " + marker);
    }
}

const readOnly = snippetsListHTML(grouped, { controls: snippetControls(direct), readOnly: true });
assert.equal((readOnly.match(/class="snippet-row" type="button" data-snippet="\d+" disabled/g)
    || []).length, 3, "a device that may read but not write sees every row, and cannot press one");
assert.ok(readOnly.includes(T.webSnippetsReadOnly), "with the reason on screen");

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

console.log("web snippet tests passed");
