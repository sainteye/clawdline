/**
 * The Projects page.
 *
 * Two halves, the same shape `Tests/web-pages.mjs` uses. The first drives the real
 * `view/projects.js` against a stand-in document: the list, one Project's worktrees, and — the
 * reason this suite exists — the three answers that are easy to draw as the same grey rectangle.
 * **An empty answer, a refused one and a truncated one are different facts**, the route went to
 * some trouble to keep them apart (`404 project_not_found`, `409 ambiguous_project`, a `read`
 * receipt on every answer that did run), and a page is where that work is most easily thrown
 * away. The second half holds the document, `main.js` and the registry against each other, which
 * is where a page nobody can reach or an element nobody defined would otherwise sit unnoticed.
 *
 * `tools/check-web-ids.py` cannot see this page's element table: it reads literal
 * `document.getElementById` calls at top level and `byId("usage-…")` through the Usage helper,
 * and every lookup here goes through `byId` under a different prefix. So the table is held
 * against `index.html` down here instead, and the check is named rather than assumed.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const read = (relative) => readFileSync(join(root, relative), "utf8");

let checks = 0;
let failed = false;
function check(condition, message) {
    checks += 1;
    if (condition) return;
    failed = true;
    console.error(`FAIL: ${message}`);
}
function say(value) {
    if (value && typeof value === "object" && typeof value.id === "string") return `#${value.id}`;
    return JSON.stringify(value);
}
function equal(actual, expected, message) {
    check(actual === expected, `${message} — expected ${say(expected)}, got ${say(actual)}`);
}
function match(actual, pattern, message) {
    check(pattern.test(String(actual)), `${message} — ${say(String(actual))} does not match ${pattern}`);
}

/* ==========================================================================
   A document, in as much as this module reads one
   ========================================================================== */

class FakeNode {
    constructor(doc, tag = "div", id = "") {
        this.ownerDocument = doc;
        this.tagName = String(tag).toUpperCase();
        this.id = id;
        this.children = [];
        this.parentNode = null;
        this.listeners = {};
        this.dataset = {};
        this.style = {};
        this.attributes = {};
        this.className = "";
        this.hidden = false;
        this.title = "";
        this._text = "";
    }
    appendChild(child) { child.parentNode = this; this.children.push(child); return child; }
    removeChild(child) {
        this.children = this.children.filter((item) => item !== child);
        child.parentNode = null;
    }
    get firstChild() { return this.children[0] || null; }
    get textContent() {
        return this._text + this.children.map((child) => child.textContent).join("");
    }
    set textContent(value) { this._text = String(value ?? ""); this.children = []; }
    addEventListener(name, handler) { (this.listeners[name] ||= []).push(handler); }
    dispatch(name, event = {}) {
        event.preventDefault ||= () => { event.defaultPrevented = true; };
        for (const handler of this.listeners[name] || []) handler(event);
    }
    click() { this.dispatch("click", {}); }
    focus(options) { this.focusCalls = (this.focusCalls || 0) + 1; this.focusOptions = options || null;
                     this.ownerDocument.activeElement = this; }
    setAttribute(name, value) { this.attributes[name] = String(value); }
    getAttribute(name) { return this.attributes[name] ?? null; }
    removeAttribute(name) { delete this.attributes[name]; }
    querySelectorAll(selector) {
        const wanted = selector.startsWith(".") ? selector.slice(1) : null;
        const found = [];
        const visit = (node) => {
            if (wanted && String(node.className).split(/\s+/).includes(wanted)) found.push(node);
            for (const child of node.children) visit(child);
        };
        visit(this);
        return found;
    }
}

class FakeDocument {
    constructor() {
        this.body = new FakeNode(this, "body", "body");
        this.activeElement = this.body;
        this.listeners = {};
    }
    createElement(tag) { return new FakeNode(this, tag); }
    addEventListener(name, handler) { (this.listeners[name] ||= []).push(handler); }
    press(key) {
        const event = { key, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; } };
        for (const handler of this.listeners.keydown || []) handler(event);
        return event;
    }
}

const flush = async () => {
    for (let i = 0; i < 4; i += 1) await new Promise((done) => setImmediate(done));
};

/* ==========================================================================
   The fixtures
   ========================================================================== */

const PLACES = {
    at: 1787067658,
    places: [
        { id: "3b9e26c1587facfd", label: "clawdline", path: "/Users/you/code/clawdline",
          icon: { accent: "#d97757", cells: [] }, at: 1787067059 },
        { id: "24f9bac626da56ea", label: "atrium", path: "/Users/you/code/atrium",
          icon: null, at: 1787066824 },
    ],
};

/* One worktree per rung, so no assertion below can pass by reading a neighbour: every count,
   every label and every date differs from every other one in this payload.

   `work` and `needs` are the two fields the "done, never landed" block was missing: what the task
   was actually doing, and which of the two things this row needs before it can leave the block.
   `label` stays the work line the classifier grouped by, which is what every card used to be
   titled with. */
function worktree(id, outcome, runs, label, extra) {
    return Object.assign({
        id, outcome, runs, tasks: [id], liveTasks: [], taskStates: [], landingStates: [],
        storedLandingStates: [], landingBasis: "live", work: null, needs: null,
        firstSeenAt: "2026-09-01T09:08:09Z", lastSeenAt: "2026-09-02T09:13:12Z",
        features: [{ id: `feature-${id.slice(0, 8)}`, label, outcome, runs, work: null }],
    }, extra || {});
}

function answer(overrides) {
    return {
        projectWorktrees: Object.assign({
            schemaVersion: 1,
            status: "available",
            policy: "one_unambiguous_accepted_head",
            outcomeRule: "landed_then_settled_then_delivered_then_live_then_abandoned",
            project: { id: "project-9c1f2e7a4b0d8e35", label: "clawdline" },
            read: { rows: 726, projectRows: 237, worktreeRows: 240, featureRows: 190,
                    truncated: false, maxScannedRows: 100000 },
            worktrees: [
                worktree("b1103ab1-6f2c-41d8-9a70-3e5c17d0ba49", "delivered", 2, "Clawdfather",
                         { work: "The landing queue's second correction",
                           needs: "land_or_abandon" }),
                worktree("4d92c7e0-1b53-4a86-b2f1-7c08e5d41a63", "delivered", 5, "Schedules page",
                         { needs: "no_record" }),
                worktree("9c077b24-67a1-4a93-ac34-40fee4c97851", "landed", 4, "Sidebar and pages"),
                worktree("5a3b90ff-2c41-4d7e-8b06-19ae5c7d3f22", "nothing_to_land", 6,
                         "Clawdfather", { work: "Independent review of the sender contract" }),
                worktree("3f9a21bc-88d0-4e57-9b12-6ca4de70f381", "active", 1, "Projects page"),
                worktree("b57fc96f-4e10-42a3-95d8-0c1b7e6a2f84", "abandoned", 3, "Delivery logs"),
                worktree("e4402d71-5c88-4b06-a3e9-71fd0b62c95a", "unknown", 7, "README"),
            ],
            excluded: { worktreesWithoutFeature: 31, reason: "no_unambiguous_accepted_head" },
            unattributed: { worktrees: 13,
                            reasons: { legacy_managed_worktree_project_key: 13 } },
        }, overrides || {}),
    };
}

function refusal(code, message) {
    return Object.assign(new Error(message), { code });
}

/* ==========================================================================
   The element table, taken from main.js rather than invented here
   ========================================================================== */

const mainSource = read("Resources/web/app/js/main.js");
const bindBlock = /bindProjectsPage\(\{([\s\S]*?)\n\}, \{/.exec(mainSource);
check(bindBlock, "main.js binds the Projects page and this suite can find the element table");
const pairs = [...(bindBlock ? bindBlock[1] : "").matchAll(/"([^"]+)":\s*byId\("([^"]+)"\)/g)];
check(pairs.length >= 25,
      `the Projects page's element table is in main.js: ${pairs.length} entries found`);
for (const [, key, id] of pairs) {
    equal(key, id, "the table's key and the id it looks up are the same string");
}

function table(doc) {
    const elements = {};
    for (const [, key] of pairs) elements[key] = new FakeNode(doc, "div", key);
    return elements;
}

/* ==========================================================================
   The module, driven
   ========================================================================== */

const moduleSource = read("Resources/web/app/js/view/projects.js");
const keysSource = read("Resources/web/app/js/input/keys.js");
const staticSource = read("Resources/web/app/js/view/static.js");
const module = await import(pathToFileURL(join(root, "Resources/web/app/js/view/projects.js")).href);
check(typeof module.bindProjectsPage === "function",
      "view/projects.js exports the executable bindProjectsPage");

const { T } = await import(pathToFileURL(join(root, "Resources/web/app/js/core/i18n.js")).href);

function harness(options = {}) {
    const doc = new FakeDocument();
    const elements = table(doc);
    const asked = [];
    const navigated = [];
    const environment = {
        document: doc,
        navigate: (name) => navigated.push(name),
        carries: options.carries === undefined ? () => true : options.carries,
        drawIcon: () => true,
        tint: () => "#d97757",
    };
    if (options.places !== null) {
        environment.places = () => { asked.push("places"); return options.places(); };
    }
    if (options.worktrees !== null) {
        environment.projectWorktrees = (path) => { asked.push(path); return options.worktrees(path); };
    }
    const page = module.bindProjectsPage(elements, environment);
    return { doc, elements, page, asked, navigated };
}

const ok = {
    places: () => Promise.resolve(PLACES),
    worktrees: () => Promise.resolve(answer()),
};

/* ---- the list ------------------------------------------------------------ */

{
    const { elements, page, asked } = harness(ok);
    equal(elements["projects-list-view"].hidden, false, "the page comes up on the list");
    equal(elements["projects-detail-view"].hidden, true, "with no Project open");
    await page.enter();
    await flush();
    equal(asked[0], "places", "arriving asks the Mac where a session could be started");
    equal(elements["projects-rows"].children.length, 2, "one row per place");
    equal(elements["projects-count"].textContent, "2", "and the count beside the heading");
    equal(elements["projects-status"].textContent, "", "with nothing left of the loading line");
    const rows = elements["projects-rows"].querySelectorAll(".project-row");
    equal(rows.length, 2, "every row is a button");
    equal(rows[0].getAttribute("aria-label"), "Open clawdline",
          "a row says what pressing it opens, because its name alone is not a sentence");
    match(rows[0].textContent, /\/Users\/you\/code\/clawdline/,
          "and carries the path, which is what tells two Projects of the same name apart");
    const marks = elements["projects-rows"].querySelectorAll(".project-row-mark");
    equal(marks.length, 2, "each row has a place for the Project's mark");
    check(!marks[0].className.includes("none"), "a Project with a mark gets it drawn");
}

{
    const { elements, page } = harness({ ...ok, places: () => Promise.resolve({ places: [] }) });
    await page.enter();
    await flush();
    equal(elements["projects-rows"].children.length, 0, "no places, no rows");
    equal(elements["projects-status"].textContent, T.webProjectsEmpty,
          "and a Mac that has never run an assistant anywhere says so rather than showing a blank");
    equal(elements["projects-count"].textContent, "",
          "a count of zero is not drawn: the sentence above already said it");
}

{
    const { elements, page } = harness({
        ...ok, places: () => Promise.reject(refusal("busy", "Busy")),
    });
    await page.enter();
    await flush();
    equal(elements["projects-status"].textContent, "Busy",
          "a refusal from /v1/places is the Mac's own words, not an empty list");
    equal(elements["projects-rows"].children.length, 0, "and nothing stale is left under it");
}

/* ---- the transport that does not carry these reads ----------------------- */

{
    const { elements, page, asked } = harness({ ...ok, carries: () => false });
    await page.enter();
    await flush();
    equal(asked.length, 0, "a transport that cannot answer is not asked");
    equal(elements["projects-status"].textContent, T.webProjectsUnavailable,
          "the page says the connection cannot read Projects rather than drawing an empty one");
}

/* ---- one Project: the whole answer --------------------------------------- */

{
    const { elements, page, asked } = harness(ok);
    await page.enter();
    await flush();
    elements["projects-rows"].querySelectorAll(".project-row")[0].click();
    await flush();
    equal(asked[1], "/Users/you/code/clawdline",
          "a Project is named to the route by its absolute path, which is the identity a place has");
    equal(elements["projects-list-view"].hidden, true, "the list steps aside");
    equal(elements["projects-detail-view"].hidden, false, "and the Project is on screen");
    equal(elements["project-name"].textContent, "clawdline", "under its own name");
    equal(elements["project-path"].textContent, "/Users/you/code/clawdline", "and its path");

    equal(elements["project-delivered"].hidden, false, "the delivered block is the one that opens");
    equal(elements["project-delivered-count"].textContent, "2",
          "carrying the count this page exists to put in front of somebody");
    equal(elements["project-delivered-title"].textContent, T.webProjectDelivered,
          "and its heading");
    equal(elements["project-delivered-list"].children.length, 2,
          "with one row per worktree that finished and never landed");
    equal(elements["project-delivered-none"].hidden, true,
          "the all-clear sentence is not drawn while there is something waiting");

    const first = elements["project-delivered-list"].children[0];
    equal(first.dataset.outcome, "delivered", "each row says which rung it is on");
    // 「光看標題真的看不出來分別」: nine cards on the real Mac read `Clawdfather — handoff
    // 18bde7c3`, which is the work line and not an answer to "what is this".
    equal(first.querySelectorAll(".project-worktree-features")[0].textContent,
          "The landing queue's second correction",
          "the heading is what the task was doing, taken from its own stored title");
    const workLine = first.querySelectorAll(".project-fact-line")[0];
    match(workLine.textContent, new RegExp(T.webProjectWorkLine),
          "and the root's label keeps its place under a word saying which of the two it is");
    match(workLine.textContent, /Clawdfather/, "carrying that label");
    match(first.textContent, /Clawdfather/, "and names the Feature it finished");
    match(first.textContent, /b1103ab1/, "and the worktree, short enough to read");
    match(first.textContent, /clawdline\/task\/b1103ab1-6f2c-41d8-9a70-3e5c17d0ba49/,
          "and the branch its delivery is on, which is the only actionable thing on this screen");
    match(first.textContent, new RegExp(T.webProjectBranch),
          "under a label saying that branch is a convention and not a stored field");

    // The block exists to be emptied, so every row in it says which of the two it needs. Nothing
    // here closes anything: a landing record is durable and terminal.
    match(first.querySelectorAll(".project-fact-needs")[0].textContent,
          new RegExp(T.webProjectNeedsLanding),
          "a row that wrote something needs a person to land it or write it off");
    const second = elements["project-delivered-list"].children[1];
    match(second.querySelectorAll(".project-fact-needs")[0].textContent,
          new RegExp(T.webProjectNeedsNoRecord),
          "and one whose task the registry has swept says there is nothing left to close");
    equal(second.querySelectorAll(".project-worktree-features")[0].textContent, "Schedules page",
          "a row with no stored title keeps the label as its heading rather than going blank");
    equal(second.querySelectorAll(".project-fact-line").length, 0,
          "and does not print that same label twice under a word saying it is something else");

    const settled = elements["project-groups"].children[1];
    equal(settled.dataset.outcome, "nothing_to_land",
          "the read-only deliveries have a rung of their own, beside landed rather than above it");
    equal(settled.children[0].textContent, T.webProjectNothingToLand + "1",
          "with the rung's name and how many are on it");
    match(settled.children[1].textContent, new RegExp(T.webProjectNothingToLandSay),
          "and the stored fact it rests on: a root recorded that nothing was written");
    equal(settled.querySelectorAll(".project-fact-needs").length, 0,
          "a settled row needs nothing, and says so by not answering");

    // The delivered worktrees are the block above; drawing them again below would double every
    // count on the page.
    const groups = elements["project-groups"].children;
    equal(groups.length, 5, "the other five rungs are the five sections underneath");
    equal(groups.map((group) => group.dataset.outcome).join(","),
          "landed,nothing_to_land,active,abandoned,unknown",
          "in the order the ladder is evaluated, hardest evidence first");
    for (const group of groups) {
        equal(group.tagName, "DETAILS",
              "each is a disclosure the browser already knows how to open, not a fold this page keeps state about");
        equal(group.children[0].tagName, "SUMMARY", "with a summary to press");
        check(!group.attributes.open, "and it comes up closed: the reading that matters is above it");
    }
    equal(groups[0].children[0].textContent, T.webProjectLanded + "1",
          "the summary is the rung's name and how many are on it");
    match(groups[0].children[1].textContent, new RegExp(T.webProjectLandedSay),
          "and the stored fact that rung rests on, rather than a description of itself");

    match(elements["project-read"].textContent, /726/, "the receipt says how much was read");
    match(elements["project-read"].textContent, /237/, "how much of it was this Project's");
    match(elements["project-read"].textContent, /240/, "how much ran in a worktree");
    match(elements["project-read"].textContent, /190/, "and how much carried a Feature");
    equal(elements["project-truncated"].hidden, true, "a complete scan raises no partial banner");
    equal(elements["project-none"].hidden, true, "and does not say it found nothing");
    equal(elements["project-excluded"].hidden, false,
          "the worktrees with no Feature are counted rather than silently dropped");
    match(elements["project-excluded"].textContent, /31/, "with their number said out loud");
    equal(elements["project-unattributed"].hidden, false,
          "and so are the ones that belong to no Project at all");
    match(elements["project-unattributed-say"].textContent, /13/,
          "which is the absence this page is not allowed to make invisible");
}

/* ---- one Project with nothing in it -------------------------------------- */

{
    const { elements, page } = harness({
        ...ok,
        worktrees: () => Promise.resolve(answer({
            worktrees: [], excluded: { worktreesWithoutFeature: 0 },
            unattributed: { worktrees: 0 },
            read: { rows: 726, projectRows: 12, worktreeRows: 0, featureRows: 0, truncated: false },
        })),
    });
    await page.enter();
    await flush();
    elements["projects-rows"].querySelectorAll(".project-row")[0].click();
    await flush();
    equal(elements["project-delivered"].hidden, true, "nothing is waiting");
    // Found by reading the page in a browser: with no worktrees at all, "nothing is waiting" sat
    // directly above "no worktree here has finished a Feature" — two sentences agreeing that there
    // is nothing, one of them implying somebody had looked through something.
    equal(elements["project-delivered-none"].hidden, true,
          "the all-clear is not drawn where there is nothing for it to be an all-clear about");
    equal(elements["project-none"].hidden, false, "the empty result is a sentence of its own");
    equal(elements["project-none"].textContent, T.webProjectNoWorktrees, "saying what was not found");
    equal(elements["project-groups"].children.length, 0, "and no rung is drawn with nothing on it");
    // **This is the assertion the whole suite is for.** An empty answer keeps its receipt, and a
    // refusal below has none: that is the only thing on screen that tells "read 726 rows and
    // found none" from "this was never answered".
    match(elements["project-read"].textContent, /726/,
          "an empty answer still carries the receipt that proves the query ran");
    equal(elements["project-status"].textContent, "", "and is not an error");
    equal(elements["project-excluded"].hidden, true, "nothing excluded, nothing said about it");
    equal(elements["project-unattributed"].hidden, true, "and nothing unattributed either");
}

/* ---- one Project where everything landed --------------------------------- */

{
    const { elements, page } = harness({
        ...ok,
        worktrees: () => Promise.resolve(answer({
            worktrees: [worktree("9c077b24-67a1-4a93-ac34-40fee4c97851", "landed", 4, "Sidebar")],
        })),
    });
    await page.enter();
    await flush();
    elements["projects-rows"].querySelectorAll(".project-row")[0].click();
    await flush();
    equal(elements["project-delivered"].hidden, true, "nothing finished here without landing");
    equal(elements["project-delivered-none"].hidden, false,
          "and with worktrees to have looked through, the all-clear is worth saying");
    equal(elements["project-delivered-none"].textContent, T.webProjectDeliveredNone,
          "in the words for it");
    equal(elements["project-none"].hidden, true, "this Project is not empty, so it is not called empty");
    equal(elements["project-groups"].children.length, 1, "and the one rung it does occupy is drawn");
}

/* ---- one Project that was refused ---------------------------------------- */

for (const [code, expected, why] of [
    ["project_not_found", T.webProjectNotFound, "404 is not an empty list"],
    ["ambiguous_project", T.webProjectAmbiguous, "409 says two Projects share the name"],
    ["usage_analytics_busy", T.webProjectBusy, "429 says nothing is wrong, ask again"],
]) {
    const { elements, page } = harness({
        ...ok, worktrees: () => Promise.reject(refusal(code, "the Mac's own sentence")),
    });
    await page.enter();
    await flush();
    elements["projects-rows"].querySelectorAll(".project-row")[0].click();
    await flush();
    equal(elements["project-status"].textContent, expected, `${code}: ${why}`);
    equal(elements["project-read"].textContent, "",
          `${code}: a refusal carries no receipt, which is how it is told from an empty answer`);
    equal(elements["project-delivered"].hidden, true, `${code}: and draws no count`);
    equal(elements["project-none"].hidden, true,
          `${code}: nor the sentence for a query that ran and matched nothing`);
}

{
    const { elements, page } = harness({
        ...ok, worktrees: () => Promise.reject(new Error("Failed to fetch")),
    });
    await page.enter();
    await flush();
    elements["projects-rows"].querySelectorAll(".project-row")[0].click();
    await flush();
    equal(elements["project-status"].textContent, "Failed to fetch",
          "an error the page has no sentence for keeps the one it was given");
}

/* ---- the scan that hit its ceiling --------------------------------------- */

{
    const { elements, page } = harness({
        ...ok,
        worktrees: () => Promise.resolve(answer({
            status: "partial",
            read: { rows: 100000, projectRows: 900, worktreeRows: 800, featureRows: 700,
                    truncated: true, maxScannedRows: 100000 },
        })),
    });
    await page.enter();
    await flush();
    elements["projects-rows"].querySelectorAll(".project-row")[0].click();
    await flush();
    equal(elements["project-truncated"].hidden, false,
          "a truncated scan says so, because everything above it is a floor and not a count");
    equal(elements["project-truncated"].textContent, T.webProjectTruncated, "in the words for it");
    equal(elements["project-delivered"].hidden, false,
          "the rows that were read are still drawn — partial is not empty");
}

/* ---- nothing of the last Project survives into the next ------------------ */

{
    let reply = () => Promise.resolve(answer({ status: "partial",
        read: { rows: 5, projectRows: 4, worktreeRows: 3, featureRows: 2, truncated: true } }));
    const { elements, page } = harness({ ...ok, worktrees: () => reply() });
    await page.enter();
    await flush();
    const rows = elements["projects-rows"].querySelectorAll(".project-row");
    rows[0].click();
    await flush();
    equal(elements["project-truncated"].hidden, false, "the first Project's scan was truncated");
    match(elements["project-read"].textContent, /5/, "and its receipt is on screen");
    reply = () => Promise.reject(refusal("project_not_found", "no"));
    rows[1].click();
    await flush();
    equal(elements["project-read"].textContent, "",
          "a refused Project must not wear the previous Project's receipt");
    equal(elements["project-truncated"].hidden, true, "nor its partial banner");
    equal(elements["project-delivered-list"].children.length, 0, "nor its worktrees");
    equal(elements["project-groups"].children.length, 0, "nor its other rungs");
}

/* ---- two answers in flight ----------------------------------------------- */

{
    const pending = [];
    const { elements, page } = harness({
        ...ok,
        worktrees: () => new Promise((done) => pending.push(done)),
    });
    await page.enter();
    await flush();
    const rows = elements["projects-rows"].querySelectorAll(".project-row");
    rows[0].click();
    rows[1].click();
    await flush();
    equal(pending.length, 2, "both Projects were asked for");
    pending[1](answer({ read: { rows: 22, projectRows: 2, worktreeRows: 2, featureRows: 2 } }));
    await flush();
    pending[0](answer({ read: { rows: 999, projectRows: 9, worktreeRows: 9, featureRows: 9 } }));
    await flush();
    match(elements["project-read"].textContent, /22/,
          "the Project on screen is the one that was asked for last, whatever order the answers arrive in");
    check(!/999/.test(elements["project-read"].textContent),
          "an answer for a Project nobody is looking at any more is dropped rather than drawn");
}

/* ---- going back, and Escape ---------------------------------------------- */

{
    const { doc, elements, page, navigated } = harness(ok);
    await page.enter();
    await flush();
    elements["projects-rows"].querySelectorAll(".project-row")[0].click();
    await flush();
    elements["projects-back"].click();
    equal(elements["projects-list-view"].hidden, false, "Back returns to the list");
    equal(elements["projects-detail-view"].hidden, true, "and puts the Project away");
    equal(doc.activeElement, elements["projects-title"],
          "with the keyboard on the heading rather than on a control that has just been hidden");

    elements["projects-rows"].querySelectorAll(".project-row")[0].click();
    await flush();
    page.escape();
    equal(elements["projects-detail-view"].hidden, true,
          "Escape over an open Project gives the Project back");
    equal(navigated.length, 0, "and only the Project — closing two things for one press is the bug");
    page.escape();
    equal(navigated[0], "sessions", "a second press leaves the page");
    equal(module.Projects.escape, page.escape,
          "and the chain that owns Escape reaches the same function this drove");
}

/* **The ordering defect a browser found and this harness could not.**
 *
 * The first version of this page answered Escape with a listener of its own, standing down while
 * the drawer was open. Driven here it was green; in Chrome one press closed the drawer *and* left
 * the page, because `input/keys.js` closes the drawer and returns — and returning is only true of
 * the listener doing it. A second listener on the same document ran afterwards and saw a drawer
 * that was already shut. A harness with one listener has nothing to be second to, so what is
 * checked instead is that there is no second listener at all. */
{
    check(!/addEventListener\("keydown"/.test(moduleSource),
          "view/projects.js binds no key listener of its own — the Escape chain is one place, and a "
          + "second listener cannot see that the first has already answered the press");
    check(/import \{ Projects \} from "\.\.\/view\/projects\.js"/.test(keysSource),
          "input/keys.js reaches this page the way it reaches Settings");
    // There is more than one `key === "Escape"` in that file — the confirmation sheet has its own
    // one-line branch — so the chain is picked by what is in it, not by which comes first.
    const escapeBlocks = [...keysSource.matchAll(/if \(key === "Escape"\) \{([\s\S]*?)\n {4}\}/g)]
        .map((found) => found[1]).filter((body) => body.includes("Sidebar.close()"));
    equal(escapeBlocks.length, 1, "exactly one Escape chain in input/keys.js decides the order");
    const chain = escapeBlocks[0] || "";
    const at = (needle) => chain.indexOf(needle);
    check(at("els.projects.hidden") > 0, "the chain has a turn for the Projects page");
    check(at("els.sidebar.hidden") < at("els.projects.hidden"),
          "after the drawer, which is over whatever page you are on");
    check(at("els.keys.hidden") < at("els.projects.hidden"),
          "and after the shortcuts card, which is over it too");
    check(at("Projects.escape()") > 0 && chain.includes("Projects.escape(); return;"),
          "and it returns, so nothing below answers the same press");
}

/* ==========================================================================
   The document, main.js and the registry — held against each other
   ========================================================================== */

const raw = read("Resources/web/index.html");
const page = raw.replace(/<!--[\s\S]*?-->/g, "");
check(page.length < raw.length, "the comment strip actually removed something");
check(!page.includes("<!--"), "and left no comment behind to be read as markup");

const defined = new Set([...page.matchAll(/\bid="([^"]+)"/g)].map((m) => m[1]));
// Calibrated against a known positive: a scan that found no ids at all would agree with every
// lookup below and say so cheerfully.
check(defined.has("app"), "the id scan finds an id that is certainly in this document");
for (const [, key] of pairs) {
    check(defined.has(key),
          `#${key} is looked up by main.js and defined in index.html — check-web-ids.py cannot see this table`);
}

const registryBlock = /Pages\.bind\(\{[\s\S]*?pages:\s*\[([\s\S]*?)\n {4}\],/.exec(mainSource);
check(registryBlock, "main.js declares the page registry");
const registered = [...(registryBlock ? registryBlock[1] : "").matchAll(/name:\s*"([^"]+)"/g)]
    .map((m) => m[1]);
equal(registered.indexOf("projects"), 1,
      "Projects is registered between Sessions and Usage: it is a way into the sessions rather than a reading about them");
check(/id="nav-projects"[^>]*data-page-to="projects"/.test(page),
      "the drawer names it, with the same attribute every other row uses");
check(/id="projects"[\s\S]{0,200}?data-page-view="projects"/.test(page),
      "and the document carries the section the router shows");
check(/hidden/.test(/<section class="page projects"[^>]*>/.exec(page)?.[0] || ""),
      "which comes up hidden, because the document is not the router");
check(/enter:\s*function\s*\(\)\s*\{\s*projects\.enter\(\)/.test(mainSource),
      "arriving at the page is what asks the Mac for the list — however the arrival happened");
check(/focus:\s*"projects-title"/.test(mainSource),
      "and the keyboard lands on the heading, which is the one thing always on this page");

for (const [id, key] of [["nav-projects", "webProjects"], ["projects-title", "webProjects"],
                         ["projects-lede", "webProjectsLede"], ["projects-back", "webProjects"]]) {
    check(new RegExp(`els\\["${id}"\\][^;]*T\\.${key}`).test(staticSource),
          `#${id} is painted from T.${key} rather than left as the English in the markup`);
}

const imports = [...moduleSource.matchAll(/^import\s[^;]*?from\s+"([^"]+)"/gm)].map((m) => m[1]);
equal(imports.join(","), "../core/i18n.js",
      "view/projects.js imports the words and nothing else, so Node can drive the whole of it");
check(!/document\.getElementById/.test(moduleSource),
      "and reaches for no element of its own: the table arrives from main.js");
/* Where the words come from. `tools/check-web-strings.py` holds `T.<name>` against the fallback
   in `core/i18n.js` and the `/v1/strings` payload, but nothing anywhere holds the page's own
   strings against the page: a sentence typed into this module instead of added to `T` crosses no
   boundary and so goes red nowhere, and the untranslated half of the Usage page is what that
   looks like a year later. The union below is what can be checked cheaply — every string this
   page's slice added is read by this module or painted by `static.js`, and neither reads one that
   does not exist. Every visible sentence is compared to its `T` value in the assertions above;
   that, and not a scan for English, is what proves this page speaks fourteen languages. */
const readsHere = new Set([...moduleSource.matchAll(/\bT\.(web[A-Za-z0-9_$]*)/g)].map((m) => m[1]));
const readsInStatic = new Set([...staticSource.matchAll(/\bT\.(webProject[A-Za-z0-9_$]*)/g)]
    .map((m) => m[1]));
check(readsHere.size >= 25, `view/projects.js draws its words from T: ${readsHere.size} of them`);
const declared = Object.keys(T).filter((key) => key.startsWith("webProject"));
equal(declared.length, 38, "this slice added thirty-eight strings to the fallback table");
for (const key of declared) {
    check(readsHere.has(key) || readsInStatic.has(key),
          `T.${key} is read by the page it was added for — a string nothing draws is a string nobody translated for a reason`);
}
for (const key of readsHere) {
    check(Object.prototype.hasOwnProperty.call(T, key),
          `T.${key} exists in core/i18n.js — a page reading a name nobody defined prints undefined`);
}

const linked = [...page.matchAll(/href="\/app\/css\/([^"]+)"/g)].map((m) => m[1]);
check(linked.includes("projects.css"), "the page's stylesheet is linked");
check(linked.indexOf("pages.css") < linked.indexOf("projects.css"),
      "after pages.css, which owns the frame this page sits in");

const liveSource = read("Resources/web/app/js/net/live.js");
check(liveSource.includes("/v1/orchestrator/usage/project-worktrees?project="),
      "the local transport carries the read this page is about");
const cloudSource = read("Resources/web/app/js/net/cloud-client.js");
check(!/projectWorktrees/.test(cloudSource),
      "and the Cloud one deliberately does not: every read a paired viewer may name carries a session, and this one's subject is a Project");
check(/typeof api\.projectWorktrees === "function"/.test(mainSource),
      "so the page asks before it draws, rather than offering a control that fails when pressed");
check(read("Resources/web/app/js/net/mock.js").includes("projectWorktrees:"),
      "the fixtures answer it too, which is what makes ?mock=1 a real walk through this page");

console.log(`${failed ? "not ok" : "ok"}: web projects page, ${checks} checks`);
if (failed) process.exit(1);
