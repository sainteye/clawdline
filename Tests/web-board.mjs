import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

class FakeNode {
    constructor(document, tag = "div", id = "") {
        this.ownerDocument = document;
        this.tagName = String(tag).toUpperCase();
        this.id = id;
        this._children = [];
        this.parentNode = null;
        this.listeners = {};
        this.attributes = {};
        this.dataset = {};
        this.className = "";
        this.hidden = false;
        this.disabled = false;
        this.checked = false;
        this.value = "";
        this.type = "";
        this.name = "";
        this.href = "";
        this.target = "";
        this.rel = "";
        this._text = "";
    }
    get children() {
        const collection = { length: this._children.length,
            item: (index) => this._children[index] || null,
            [Symbol.iterator]: () => this._children[Symbol.iterator]() };
        this._children.forEach((child, index) => { collection[index] = child; });
        return collection;
    }
    appendChild(child) { child.parentNode = this; this._children.push(child); return child; }
    removeChild(child) {
        this._children = this._children.filter((candidate) => candidate !== child);
        child.parentNode = null;
    }
    get firstChild() { return this._children[0] || null; }
    get textContent() { return this._text + this._children.map((child) => child.textContent).join(""); }
    set textContent(value) { this._text = String(value ?? ""); this._children = []; }
    addEventListener(type, callback) { (this.listeners[type] ||= []).push(callback); }
    dispatch(type, event = {}) {
        event.target ||= this;
        event.currentTarget = this;
        event.preventDefault ||= () => { event.defaultPrevented = true; };
        for (const callback of this.listeners[type] || []) callback(event);
    }
    click() { this.dispatch("click"); }
    focus() { this.ownerDocument.activeElement = this; }
    setAttribute(name, value) {
        this.attributes[name] = String(value);
        if (name.startsWith("data-")) {
            this.dataset[name.slice(5).replace(/-([a-z])/g, (_, letter) => letter.toUpperCase())] = String(value);
        }
    }
    getAttribute(name) { return this.attributes[name] ?? null; }
    matches(selector) {
        if (selector.startsWith(".")) return this.className.split(/\s+/).includes(selector.slice(1));
        if (selector.startsWith("[data-board-action=")) {
            return this.dataset.boardAction === selector.match(/"([^"]+)"/)[1];
        }
        return this.tagName === selector.toUpperCase();
    }
    all(predicate, output = []) {
        if (typeof predicate === "string" ? this.matches(predicate) : predicate(this)) output.push(this);
        for (const child of this._children) child.all(predicate, output);
        return output;
    }
}

class FakeDocument {
    constructor(lang = "zh-Hant") {
        this.documentElement = { lang };
        this.body = new FakeNode(this, "body", "body");
        this.activeElement = this.body;
    }
    createElement(tag) { return new FakeNode(this, tag); }
}

function deferred() {
    let resolve, reject;
    const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
    return { promise, resolve, reject };
}

const flush = async () => {
    for (let index = 0; index < 5; index += 1) await new Promise((done) => setImmediate(done));
};

const TYPES = ["feature", "refactor", "task", "bug", "coordination", "epic"];
function item(type, index) {
    return {
        id: `item-${index}`, key: `CLD-${index}`, projectId: "project-a",
        title: `${type} <unsafe>`, type, state: index === 2 ? "execution" : "backlog",
        summary: `summary-${index}`, owner: `owner-${index}`, parentId: null,
        createdAt: 1788800000 + index, updatedAt: 1788800100 + index,
        checklist: [], milestones: [], artifacts: [], links: [], obligations: [], history: [], spans: []
    };
}
const ITEMS = TYPES.map(item);
const DETAIL = Object.assign({}, ITEMS[0], {
    state: "execution",
    checklist: [{ id: "check-1", title: "Keyboard works", status: "doing", required: true }],
    milestones: [{ id: "mile-1", title: "Mobile ready", status: "todo" }],
    artifacts: [
        { id: "artifact-safe", title: "Release notes", url: "https://example.test/release", kind: "document" },
        { id: "artifact-bad", title: "Bad scheme", url: "javascript:alert(1)", kind: "other" }
    ],
    links: [
        { id: "link-session", kind: "session", targetId: "session-1", label: "Build session" },
        { id: "link-task", kind: "task", targetId: "task-1", label: "Delivery task" },
        { id: "link-tree", kind: "worktree", targetId: "tree-1", label: "Feature worktree" }
    ],
    obligations: [{ id: "owed-1", title: "Choose rollout", owner: "user", blocking: true, resolved: false }],
    history: [{ id: "history-1", at: 1788800200, actor: "root", kind: "transition", summary: "Started output" }],
    spans: [{ id: "span-1", sessionId: "session-1", phase: "output", startedAt: 1788800200, endedAt: null }]
});

function envelope(overrides = {}) {
    return { board: Object.assign({
        schemaVersion: 1, revision: 9, enabled: true, mode: "board",
        entitlement: { state: "free_preview", label: "Currently free" },
        viewer: { id: "viewer", canWrite: true, canManage: true },
        projects: [{ id: "project-a", name: "Clawdline", itemCount: ITEMS.length }],
        items: ITEMS, item: null, truncated: false, updatedAt: 1788800300
    }, overrides) };
}

function page(environment = {}, lang = "zh-Hant") {
    const document = new FakeDocument(lang);
    const ids = ["board", "board-projects", "board-items", "board-detail", "board-status",
        "board-new", "board-search", "board-layout", "board-type", "board-state", "board-back", "board-refresh"];
    const elements = {};
    for (const id of ids) elements[id] = document.body.appendChild(new FakeNode(document, "div", id));
    elements["board-projects"].tagName = "SELECT";
    elements["board-new"].tagName = "BUTTON";
    elements["board-search"].tagName = "INPUT";
    elements["board-layout"].tagName = "BUTTON";
    elements["board-type"].tagName = "SELECT";
    elements["board-state"].tagName = "SELECT";
    elements["board-back"].tagName = "BUTTON";
    return { document, elements, environment: Object.assign({ document }, environment) };
}

const boardModule = await import(pathToFileURL(join(root, "Resources/web/app/js/view/board.js")).href);
assert.equal(typeof boardModule.bindBoardPage, "function", "board view exports bindBoardPage");

let checks = 1;
function count(assertion) { checks += 1; return assertion; }

{
    const { document, elements, environment } = page({
        read() { return Promise.resolve(envelope()); },
        command() { return Promise.reject(new Error("not used")); }
    }, "en");
    const view = boardModule.bindBoardPage(elements, environment);
    document.documentElement.lang = "zh-Hant"; // applyStrings runs after modules are bound.
    await view.enter();
    count(assert.match(elements["board-type"].textContent, /功能/,
        "enter recomputes localized filter labels after the central strings arrive"));
    count(assert.match(elements["board-state"].textContent, /規劃中/,
        "lifecycle filter options use reader-facing labels rather than wire values"));
}

{
    const reads = [];
    const modes = [];
    const projects = [];
    const navigations = [];
    const opened = [];
    const { document, elements, environment } = page({
        read(projectId, itemId) {
            reads.push([projectId, itemId]);
            return Promise.resolve(itemId ? envelope({ items: ITEMS, item: DETAIL }) : envelope());
        },
        command() { return Promise.reject(new Error("not used")); },
        navigate(...parts) { navigations.push(parts); },
        openSession(id) { opened.push(id); },
        onMode(board) { modes.push(board.mode); },
        onProjects(value) { projects.push(value); }
    });
    const view = boardModule.bindBoardPage(elements, environment);
    count(assert.equal(typeof view.open, "function", "the shell can open Project and item deep links"));
    count(assert.doesNotThrow(() => view.enter(), "enter is callable"));
    await flush();
    count(assert.deepEqual(reads, [[undefined, undefined]], "entry reads the board snapshot"));
    count(assert.deepEqual(modes, ["board"], "the owning shell learns the workflow mode"));
    count(assert.equal(projects[0][0].name, "Clawdline", "the owning shell receives projects"));
    count(assert.match(elements["board-projects"].textContent, /Clawdline/, "projects are selectable"));

    elements["board-projects"].value = "project-a";
    elements["board-projects"].dispatch("change");
    await flush();
    count(assert.deepEqual(reads.at(-1), ["project-a", undefined], "choosing a project reads its items"));
    for (const type of TYPES) {
        count(assert.ok(elements["board-items"].all((node) => node.dataset.boardType === type).length,
            `the ${type} item is rendered as its own type`));
    }
    count(assert.equal((elements["board-items"].textContent.match(/<unsafe>/g) || []).length, 6,
        "payload angle brackets survive as literal text on all six cards"));

    elements["board-search"].value = "coordination";
    elements["board-search"].dispatch("input");
    count(assert.equal(elements["board-items"].all(".board-item-card").length, 1,
        "search filters the rendered item collection"));
    elements["board-search"].value = "";
    elements["board-search"].dispatch("input");
    elements["board-type"].value = "bug";
    elements["board-type"].dispatch("change");
    count(assert.equal(elements["board-items"].all(".board-item-card").length, 1,
        "type filtering is behavioral"));
    elements["board-type"].value = "all";
    elements["board-type"].dispatch("change");
    elements["board-state"].value = "execution";
    elements["board-state"].dispatch("change");
    count(assert.equal(elements["board-items"].all(".board-item-card").length, 1,
        "state filtering is behavioral"));
    elements["board-state"].value = "all";
    elements["board-state"].dispatch("change");
    elements["board-layout"].click();
    count(assert.ok(elements["board-items"].all(".board-column").length >= 4,
        "desktop board mode groups items into named lifecycle columns"));

    elements["board-layout"].click();
    const firstCard = elements["board-items"].all(".board-item-card")[0];
    firstCard.click();
    await flush();
    count(assert.deepEqual(reads.at(-1), ["project-a", "item-0"], "opening an item requests detail"));
    count(assert.match(elements["board-detail"].textContent, /Keyboard works/, "checklist is visible"));
    count(assert.match(elements["board-detail"].textContent, /Mobile ready/, "milestones are visible"));
    count(assert.match(elements["board-detail"].textContent, /Choose rollout/, "obligations are visible"));
    count(assert.match(elements["board-detail"].textContent, /Started output/, "history is visible"));
    count(assert.match(elements["board-detail"].textContent, /用量未知|Usage unknown/, "missing usage is not rendered as zero"));
    const safeArtifact = elements["board-detail"].all((node) => node.tagName === "A" && node.textContent === "Release notes")[0];
    count(assert.equal(safeArtifact.href, "https://example.test/release", "safe artifacts are openable"));
    count(assert.equal(safeArtifact.rel, "noopener noreferrer", "external artifacts cannot retain an opener"));
    count(assert.equal(elements["board-detail"].all((node) => node.tagName === "A" && /Bad scheme/.test(node.textContent)).length, 0,
        "executable artifact schemes never become links"));
    elements["board-detail"].all((node) => node.dataset.boardAction === "open-session")[0].click();
    count(assert.deepEqual(opened, ["session-1"], "session links use the authenticated session opener"));
    count(assert.ok(elements["board-detail"].all((node) => node.dataset.boardLinkKind === "task").length,
        "task links remain visible typed references"));
    count(assert.ok(elements["board-detail"].all((node) => node.dataset.boardLinkKind === "worktree").length,
        "worktree links remain visible typed references"));
    count(assert.ok(navigations.length >= 2, "project and item selections update navigation state"));
    view.escape();
    count(assert.equal(elements["board-detail"].hidden, true, "Escape returns from detail to its item list"));
    view.escape();
    count(assert.equal(elements["board-items"].hidden, true, "a second Escape returns to projects"));
}

{
    const reads = [];
    const partial = Object.assign({}, DETAIL, { usage: {
        state: "partial", rows: 3, measured: 120, total: null, output: null,
        parts: { inputNew: 80, output: null, cacheRead: 40, cacheWrite: null },
        incompleteRows: 1, phases: {}, undeclaredRows: 2,
        phaseReason: "usage_boundary_unavailable", costSeries: [], missingCostRows: 3
    } });
    const { elements, environment } = page({
        read(projectId, itemId) {
            reads.push([projectId, itemId]);
            return Promise.resolve(envelope({ item: partial }));
        },
        command() { return Promise.reject(new Error("not used")); }
    });
    const view = boardModule.bindBoardPage(elements, environment);
    await view.open("project-a", "item-0");
    const usage = elements["board-detail"].all(".board-section")[0];
    count(assert.deepEqual(reads, [["project-a", "item-0"]], "open reads the exact deep-linked subject"));
    count(assert.match(usage.textContent, /120/, "partial usage is displayed as a measured floor"));
    count(assert.match(usage.textContent, /未宣告|Undeclared/, "missing phase proof remains visibly undeclared"));
    count(assert.doesNotMatch(usage.textContent, /總計[^\n]*0|Total[^\n]*0/, "partial usage never invents an exact zero total"));
}

{
    const first = deferred();
    const second = deferred();
    const reads = [];
    const { elements, environment } = page({
        read(projectId, itemId) {
            reads.push([projectId, itemId]);
            if (!projectId) return Promise.resolve(envelope());
            if (!itemId) return Promise.resolve(envelope());
            return itemId === "item-0" ? first.promise : second.promise;
        },
        command() { return Promise.reject(new Error("not used")); }
    });
    const view = boardModule.bindBoardPage(elements, environment);
    await view.enter();
    elements["board-projects"].value = "project-a";
    elements["board-projects"].dispatch("change");
    await flush();
    const cards = elements["board-items"].all(".board-item-card");
    cards[0].click();
    cards[1].click();
    second.resolve(envelope({ item: Object.assign({}, ITEMS[1], { title: "Newest detail" }) }));
    await flush();
    first.resolve(envelope({ item: Object.assign({}, ITEMS[0], { title: "Stale detail" }) }));
    await flush();
    count(assert.match(elements["board-detail"].textContent, /Newest detail/, "newest selection wins a read race"));
    count(assert.doesNotMatch(elements["board-detail"].textContent, /Stale detail/, "late stale detail cannot repaint"));
}

{
    const calls = [];
    let attempt = 0;
    const { elements, environment } = page({
        read(projectId, itemId) {
            return Promise.resolve(itemId ? envelope({ item: DETAIL }) : envelope());
        },
        command(body) {
            calls.push({ ...body });
            attempt += 1;
            if (attempt === 1) return Promise.reject(Object.assign(new Error("busy"), { code: "busy" }));
            return Promise.resolve(envelope({ revision: 12, item: DETAIL }));
        }
    });
    const view = boardModule.bindBoardPage(elements, environment);
    await view.enter();
    elements["board-projects"].value = "project-a";
    elements["board-projects"].dispatch("change");
    await flush();
    elements["board-new"].click();
    const createForm = elements["board-items"].all((node) => node.dataset.boardForm === "create")[0];
    createForm.all((node) => node.name === "title")[0].value = "A durable task";
    createForm.all((node) => node.name === "type")[0].value = "task";
    createForm.dispatch("submit");
    await flush();
    count(assert.equal(calls[0].operation, "create", "inline create submits a typed command"));
    count(assert.equal(calls[0].expectedRevision, 9, "mutation uses the displayed CAS revision"));
    count(assert.ok(calls[0].requestId, "mutation has a nonempty idempotency request id"));
    count(assert.match(elements["board-status"].textContent, /busy|忙碌/i, "a failed action remains visibly failed"));
    view.state.revision = 11; // A background snapshot arrived while this transient action waited.
    elements["board-status"].all((node) => node.dataset.boardAction === "retry")[0].click();
    await flush();
    count(assert.equal(calls[1].requestId, calls[0].requestId, "transient retry reuses the action request id"));
    count(assert.deepEqual(calls[1], calls[0], "an idempotent retry keeps the entire normalized command identical"));
    count(assert.equal(view.state.revision, 12, "successful command adopts the returned board revision"));
}

{
    const calls = [];
    let reads = 0;
    const { elements, environment } = page({
        read(projectId, itemId) {
            reads += 1;
            if (reads >= 3) return Promise.resolve(envelope({ revision: 12, item: DETAIL }));
            return Promise.resolve(itemId ? envelope({ item: DETAIL }) : envelope());
        },
        command(body) {
            calls.push({ ...body });
            if (calls.length === 1) {
                return Promise.reject(Object.assign(new Error("changed elsewhere"), { code: "revision_conflict" }));
            }
            return Promise.resolve(envelope({ revision: 13, item: DETAIL }));
        }
    });
    const view = boardModule.bindBoardPage(elements, environment);
    await view.enter();
    elements["board-projects"].value = "project-a";
    elements["board-projects"].dispatch("change");
    await flush();
    elements["board-items"].all(".board-item-card")[0].click();
    await flush();
    elements["board-detail"].all((node) => node.dataset.boardAction === "edit")[0].click();
    const edit = elements["board-detail"].all((node) => node.dataset.boardForm === "edit")[0];
    edit.all((node) => node.name === "summary")[0].value = "New summary";
    edit.dispatch("submit");
    await flush();
    count(assert.equal(view.state.revision, 12, "CAS conflict refreshes the selected item's current revision"));
    const retry = elements["board-status"].all((node) => node.dataset.boardAction === "retry")[0];
    retry.click();
    await flush();
    count(assert.notEqual(calls[1].requestId, calls[0].requestId, "CAS rebase creates a new request identity"));
    count(assert.equal(calls[1].expectedRevision, 12, "CAS retry uses the refreshed revision"));
}

{
    const commands = [];
    const taskDetail = Object.assign({}, DETAIL, { type: "task" });
    const { elements, environment } = page({
        read(projectId, itemId) {
            return Promise.resolve(itemId ? envelope({ item: taskDetail }) : envelope());
        },
        command(body) { commands.push(body); return Promise.resolve(envelope({ revision: 10, item: DETAIL })); }
    });
    const view = boardModule.bindBoardPage(elements, environment);
    await view.enter();
    elements["board-projects"].value = "project-a";
    elements["board-projects"].dispatch("change");
    await flush();
    elements["board-items"].all(".board-item-card")[0].click();
    await flush();
    elements["board-detail"].all((node) => node.dataset.boardAction === "accept-artifact")[0].click();
    await flush();
    for (const operation of ["checklist", "milestone", "obligation", "handoff", "span"]) {
        const form = elements["board-detail"].all((node) => node.dataset.boardForm === operation)[0];
        for (const field of form.all((node) => !!node.name)) {
            if (field.name === "title") field.value = `${operation} title`;
            if (field.name === "owner") field.value = "next-owner";
            if (field.name === "note") field.value = "handoff note";
            if (field.name === "sessionId") field.value = "session-new";
            if (field.name === "phase") field.value = "review_testing";
        }
        form.dispatch("submit");
        await flush();
    }
    elements["board-detail"].all((node) => node.dataset.boardAction === "resolve-obligation")[0].click();
    await flush();
    count(assert.deepEqual(commands.map((body) => body.operation),
        ["accept_artifact", "checklist", "milestone", "obligation", "handoff", "span", "resolve_obligation"],
        "detail forms and controls emit every collaboration command without browser dialogs"));
}

{
    const { elements, environment } = page({
        read() { return Promise.resolve(envelope({ enabled: false, mode: "standard", item: DETAIL })); },
        command() { throw new Error("read-only view must not command"); }
    }, "en");
    const view = boardModule.bindBoardPage(elements, environment);
    await view.enter();
    count(assert.match(elements["board-status"].textContent, /read.only|standard mode/i,
        "disabled mode explains that history remains read-only"));
    count(assert.equal(elements["board-new"].disabled, true, "disabled mode removes create authority"));
    count(assert.match(elements["board-projects"].textContent, /Clawdline/,
        "disabled mode keeps board history discoverable"));
}

{
    const commands = [];
    const detail = Object.assign({}, DETAIL, { handoff: { proposedOwner: "viewer-1", note: "Continue delivery" } });
    const { elements, environment } = page({
        read() { return Promise.resolve(envelope({ item: detail, viewer: { id: "viewer-1", canWrite: true, canManage: false } })); },
        command(body) { commands.push(body); return Promise.resolve(envelope({ item: detail, revision: 10 })); }
    });
    const view = boardModule.bindBoardPage(elements, environment);
    await view.open("project-a", DETAIL.id);
    const form = elements["board-detail"].all((node) => node.dataset.boardForm === "accept_handoff")[0];
    count(assert.ok(form, "authenticated proposed receiver gets an acceptance form"));
    form.all((node) => node.name === "note")[0].value = "Ownership accepted";
    form.dispatch("submit");
    await flush();
    count(assert.equal(commands[0].operation, "accept_handoff", "acceptance is not a closure transition"));
    count(assert.equal(commands[0].itemId, DETAIL.id, "acceptance names the exact work item"));
    environment.read = () => Promise.resolve(envelope({ item: detail, viewer: { id: "other-viewer" } }));
    const other = page({ read: environment.read });
    await boardModule.bindBoardPage(other.elements, other.environment).open("project-a", DETAIL.id);
    count(assert.equal(other.elements["board-detail"].all((node) => node.dataset.boardForm === "accept_handoff").length, 0,
        "unrelated viewer cannot claim receiver acceptance"));
}
const correctionFailures = [];
async function correctionCase(name, body) {
    try { await body(); } catch (error) { correctionFailures.push(`${name}: ${error.message}`); }
}
await correctionCase("PB-SPEC-05 evidence projection", async () => {
    const detail = { ...DETAIL, type: "task", currentEvidence: { verificationId: "v2", landingId: "l2", artifactAcceptanceId: "a2" },
        verifications: [{ id: "v1", status: "passed", subject: "old-tree", summary: "Old acceptance", source: "root_attestation" },
            { id: "v2", status: "passed", subject: "current-tree", summary: "Current acceptance", source: "root_attestation" }],
        landings: [{ id: "l2", status: "passed", subject: "current-tree", summary: "Exact landing", source: "broker" }],
        artifactAcceptances: [{ id: "a2", artifactId: "artifact-safe", status: "passed", subject: "artifact-safe", summary: "Accepted output", source: "root_attestation" }],
        findings: [{ id: "f1", status: "open", summary: "Visible blocking finding", subject: "current-tree", blocking: true, resolved: false, source: "broker" }] };
    const { elements, environment } = page({ read: async () => envelope({ item: detail }) }, "en");
    await boardModule.bindBoardPage(elements, environment).open("project-a", detail.id);
    const text = elements["board-detail"].textContent;
    count(assert.match(text, /Current acceptance.*current-tree/s, "actual plural verification payload is readable"));
    count(assert.match(text, /Exact landing/, "actual plural landing payload is readable"));
    count(assert.match(text, /Visible blocking finding/, "finding summary is not reduced to a count"));
    count(assert.match(text, /Historical|Stale/i, "old acceptance cannot appear current"));
    count(assert.equal(elements["board-detail"].all(n => n.dataset.boardAction === "accept-artifact" && n.dataset.boardArtifactId === "artifact-safe").length, 0,
        "current artifact acceptance removes redundant accept action"));
});
await correctionCase("PB-SPEC-06 capability", async () => {
    for (const canWrite of [false, true]) {
        const detail = { ...DETAIL, type: "task" };
        const { elements, environment } = page({ read: async () => envelope({ item: detail,
            viewer: { id: "reader", canWrite, canManage: false } }) });
        await boardModule.bindBoardPage(elements, environment).open("project-a", detail.id);
        count(assert.equal(elements["board-detail"].all(n => n.dataset.boardAction === "edit").length, canWrite ? 1 : 0, "writer and reader have distinct controls"));
        count(assert.equal(elements["board-detail"].all(n => n.dataset.boardAction === "accept-artifact").length, 0, "writer is not manager"));
        if (!canWrite) count(assert.equal(elements["board-detail"].all(n => !!n.dataset.boardForm).length, 0, "read-only detail has no mutation forms"));
    }
});
await correctionCase("PB-REPO-01 qualified measurements", async () => {
    const detail = { ...DETAIL, usage: { state: "qualified", total: 123, measured: 123, coverageReasons: ["session_unresolved"] } };
    const { elements, environment } = page({ read: async () => envelope({ item: detail }) }, "en");
    await boardModule.bindBoardPage(elements, environment).open("project-a", detail.id);
    const text = elements["board-detail"].all(".board-usage-value")[0].textContent;
    count(assert.match(text, /123/, "qualified observation remains visible"));
    count(assert.doesNotMatch(text, /Total|≥/, "possibly duplicated observation is neither exact total nor floor"));
});
await correctionCase("PB-RUNTIME-02 serialized commands", async () => {
    const first = deferred(), calls = [];
    const { elements, environment } = page({ read: async () => envelope({ item: DETAIL }),
        command: body => { calls.push(body); return calls.length === 1 ? first.promise : Promise.resolve(envelope({ item: DETAIL, revision: 10 })); } });
    const view = boardModule.bindBoardPage(elements, environment);
    await view.open("project-a", DETAIL.id);
    const form = elements["board-detail"].all(n => n.dataset.boardForm === "milestone")[0];
    form.all(n => n.name === "title")[0].value = "First change";
    form.dispatch("submit"); form.dispatch("submit");
    await flush();
    count(assert.equal(calls.length, 1, "second submission cannot overwrite the active receipt"));
    first.reject(Object.assign(new Error("changed"), { code: "revision_conflict" }));
    await flush();
    elements["board-status"].all(n => n.dataset.boardAction === "retry")[0].click();
    await flush();
    count(assert.equal(calls.length, 2, "rebase retry can settle without null pending crash"));
    count(assert.notEqual(calls[0].requestId, calls[1].requestId, "known CAS rejection gets a new identity"));
});
await correctionCase("PB-SPEC-07 durable Session resolution", async () => {
    count(assert.deepEqual(boardModule.resolveBoardSession([{ id: "%42", sessionId: "conversation" }], "conversation"), { id: "%42" }));
    count(assert.equal(boardModule.resolveBoardSession([{ id: "%42", sessionId: "different" }], "conversation").error, "session_unavailable"));
    count(assert.equal(boardModule.resolveBoardSession([{ id: "%42", sessionId: "conversation" }, { id: "%43", sessionId: "conversation" }], "conversation").error, "session_ambiguous"));
    const { elements, environment } = page({ read: () => Promise.resolve(envelope({ item: DETAIL })), openSession: () => ({ error: "session_unavailable" }) }, "en");
    await boardModule.bindBoardPage(elements, environment).open("project-a", DETAIL.id);
    elements["board-detail"].all(node => node.dataset.boardAction === "open-session")[0].click();
    count(assert.match(elements["board-status"].textContent, /no unique live Session/i));
});
await correctionCase("PB-RUNTIME-03/04 projection coverage is visible", async () => {
    const detail = { ...DETAIL, historyDroppedCount: 101, projection: { history: { retainedCount: 2, omittedCount: 99, reason: "projection_limit" } } };
    const { elements, environment } = page({ read: () => Promise.resolve(envelope({ item: detail, source: { ingestion: { status: "partial", issueCount: 3, reasons: ["link_capacity"] } } })) }, "en");
    await boardModule.bindBoardPage(elements, environment).open("project-a", detail.id);
    count(assert.match(elements["board-detail"].textContent, /99.*omitted/i));
    count(assert.match(elements["board-detail"].textContent, /2.*no longer retained/i));
    count(assert.match(elements["board-status"].textContent, /link_capacity/i));
});
await correctionCase("explicit board refresh", async () => {
    let reads = 0;
    const { elements, environment } = page({ read: () => Promise.resolve(envelope({ revision: 9 + reads++ })) }, "en");
    const view = boardModule.bindBoardPage(elements, environment);
    await view.enter("project-a");
    elements["board-refresh"].click(); await flush();
    count(assert.equal(view.state.revision, 10, "explicit refresh observes updated broker projection"));
});
assert.deepEqual(correctionFailures, [], correctionFailures.join("\n"));
console.log(`${checks} web board behavioral checks passed`);
