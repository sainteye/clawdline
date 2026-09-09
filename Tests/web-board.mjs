import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { bindBoardPage, boardProgress, resolveBoardSession } from "../Resources/web/app/js/view/board.js";
import * as boardModule from "../Resources/web/app/js/view/board.js";
const {bindSessionBoard, SessionBoard} = await import("../Resources/web/app/js/input/session-board.js");

class Node {
    constructor(doc, tag = "div") {
        this.ownerDocument = doc;
        this.tagName = tag.toUpperCase();
        this.children = [];
        this.listeners = {};
        this.attributes = {};
        this.dataset = {};
        this.className = "";
        this.hidden = false;
        this.value = "";
        this._text = "";
    }
    get firstChild() {
        return this.children[0];
    }
    get textContent() {
        return this._text + this.children.map((row) => row.textContent).join("");
    }
    set textContent(value) {
        this._text = String(value);
        this.children = [];
    }
    appendChild(child) {
        this.children.push(child);
        child.parentNode = this;
        return child;
    }
    removeChild(child) {
        this.children = this.children.filter((row) => row !== child);
    }
    addEventListener(name, callback) {
        (this.listeners[name] ||= []).push(callback);
    }
    setAttribute(name, value) {
        this.attributes[name] = value;
    }
    getAttribute(name) {
        return this.attributes[name];
    }
    dispatch(name) {
        for (const callback of this.listeners[name] || []) callback({ target: this });
    }
    click() {
        this.dispatch("click");
    }
    all(predicate) {
        if (typeof predicate === "string") {
            const cls = predicate.slice(1);
            predicate = (row) => row.className.split(" ").includes(cls);
        }
        return [this, ...this.children.flatMap((row) => row.all(() => true))].filter(predicate);
    }
}
function document(lang = "zh-Hant") {
    const doc = { documentElement: { lang }, hidden: false };
    doc.createElement = (tag) => new Node(doc, tag);
    doc.createElementNS = (_namespace, tag) => new Node(doc, tag);
    return doc;
}
function deferred() {
    let resolve, reject;
    const promise = new Promise((a, b) => {
        resolve = a;
        reject = b;
    });
    return { promise, resolve, reject };
}
async function flush() {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setImmediate(resolve));
}
let checks = 0;
function check(name, condition) {
    assert.ok(condition, name);
    checks++;
}
function item(id, progress, projectId = "a") {
    return {
        id,
        projectId,
        title: id + " <literal>",
        type: "feature",
        state: "backlog",
        progress: { state: progress },
        updatedAt: 1700000000,
        checklist: [],
        links: [],
        artifacts: [],
        history: [],
        obligations: []
    };
}
const rows = [
    item("active", "execution"),
    item("done", "landed"),
    item("stopped", "canceled"),
    item("delivered", "delivered"),
    item("foreign", "execution", "b")
];
function envelope(overrides = {}) {
    return {
        board: {
            schemaVersion: 1,
            revision: 1,
            enabled: true,
            projects: [
                { id: "a", name: "Clawdline", itemCount: 4 },
                { id: "b", name: "Other", itemCount: 1 }
            ],
            items: rows,
            item: null,
            viewer: { canWrite: true, canManage: true },
            ...overrides
        }
    };
}
function page(env = {}, lang = "zh-Hant") {
    const doc = document(lang),
        elements = {};
    for (const id of [
        "board",
        "board-title",
        "board-subtitle",
        "board-items",
        "board-detail",
        "board-status",
        "board-search",
        "board-back",
        "board-refresh"
    ])
        elements[id] = new Node(doc);
    const timers = new Map();
    let next = 0;
    const environment = {
        document: doc,
        read: () => Promise.resolve(envelope()),
        setTimeout: (fn) => {
            timers.set(++next, fn);
            return next;
        },
        clearTimeout: (id) => timers.delete(id),
        ...env
    };
    return {
        doc,
        elements,
        environment,
        timers,
        view: bindBoardPage(elements, environment)
    };
}
const html = readFileSync(new URL("../Resources/web/index.html", import.meta.url), "utf8");
{
    const doc = document("zh-Hant"), container = new Node(doc);
    let visible = true, ready = false, reads = 0;
    const conversation = "11111111-1111-4111-8111-111111111111";
    const destination = [];
    bindSessionBoard(container, {
        visible: () => visible, ready: () => ready,
        read: async () => { reads++; return envelope({sessionId: conversation,
            items: [{id:"linked",projectId:"a",title:"<script>只是文字</script>",sessionActivity:"declared"}],
            readState: {status:"ready"}}); },
        open: (...args) => destination.push(args)
    });
    SessionBoard.follow({id:"%late",sessionId:null});SessionBoard.setEnabled(true);
    await flush();
    check("Session relationship binder waits for provider identity and transcript", container.hidden && reads === 0);
    ready = true;
    SessionBoard.sync({id:"%late",sessionId:conversation});await flush();
    check("late identity begins one optional relation read", !container.hidden && reads === 1);
    check("Session relationship copy honors zh-Hant", container.textContent.includes("看板項目") && container.textContent.includes("更新關聯"));
    const heading = container.children[0]; heading.click();
    check("relationship disclosure exposes its accessible state", heading.getAttribute("aria-expanded") === "true" && !container.children[1].hidden);
    check("relationship titles remain literal text", container.textContent.includes("<script>只是文字</script>") && !container.all(n => n.tagName === "SCRIPT").length);
    container.children[1].all(n => n.tagName === "BUTTON")[0].click();
    check("relationship click carries exact owning Project", destination[0][0] === "a" && destination[0][1] === "linked" && destination[0][2].id === "a");
    SessionBoard.sync({id:"%late",sessionId:conversation});await flush();
    check("ordinary Session redraw does not poll optional relationships", reads === 1);
    visible = false;SessionBoard.sync({id:"%late",sessionId:conversation});
    check("another page hides Session context", container.hidden);
    SessionBoard.setEnabled(false);
    visible = true;SessionBoard.sync({id:"%late",sessionId:conversation});await flush();
    check("disabled Board stays hidden across navigation", container.hidden && reads === 1);
}
check("no manual New item entry exists", !html.includes('id="board-new"'));
check("no lifecycle selector exists", !html.includes('id="board-state"'));
const css = readFileSync(new URL("../Resources/web/app/css/board.css", import.meta.url), "utf8");
check("hidden search overrides its ID-level display rule", /display:\s*none/.test(css.match(/#board-search\[hidden\]\s*\{([^}]*)\}/)?.[1] || ""));
{
    const opened = [], destinations = [];
    const shell = { state: { projectId: null }, enter: () => opened.push("selected") };
    check("project-contained entry seam exists", typeof boardModule.enterProjectBoard === "function");
    if (boardModule.enterProjectBoard) {
        await boardModule.enterProjectBoard(shell, destination => destinations.push(destination));
        check("cold Board URL resolves to the sole Project catalog", destinations[0] === "projects" && opened.length === 0);
        shell.state.projectId = "a";
        await boardModule.enterProjectBoard(shell, destination => destinations.push(destination));
        check("selected Project still enters its own board", opened.length === 1 && destinations.length === 1);
    }
}
check("completion report has a phone-width layout boundary", /@media\s*\(max-width:\s*640px\)[\s\S]*\.board-report-body\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)/.test(css));
check("bounded expansion controls keep a full-width phone tap target",
    /@media\s*\(max-width:\s*640px\)[\s\S]*\.board-load-more\s*\{[^}]*width:\s*100%/.test(css));
check("explicit server progress wins old backlog", boardProgress(rows[1]) === "landed");
{
    const p = page({ read: async () => envelope({ projects: [], items: [], readState: { status: "error" } }) }, "en");
    await p.view.open("a");
    check("failed first materialization does not claim to display previous records",
        !p.elements["board-status"].textContent.includes("last available"));
    p.view.leave();
}
check(
    "unknown server status is not called complete",
    boardProgress({
        state: "closed",
        progress: { state: "new-server-status" }
    }) === "unknown"
);
check("task success is not landing", boardProgress({ state: "backlog", success: true }) === "planning");
{
    const kinds = ["feature", "refactor", "task", "bug", "coordination", "epic"];
    const p = page({ read: async () => envelope({ items: kinds.map(type => ({
        ...item(type, "execution"), type, progress: { state: "execution", group: "active" }
    })) }) });
    await p.view.open("a");
    const coordination = p.elements["board-items"].all(".board-coordination-group")[0];
    if (coordination) { coordination.open = true; coordination.dispatch("toggle"); }
    const cards = p.elements["board-items"].all(".board-item-card");
    check("every execution type has a visible icon and text label", cards.length === 6 &&
        cards.every(card => card.all(".board-kind").length === 1 && card.all(".board-kind-label")[0]?.textContent));
    check("type identity leads on the left before the separate progress marker", cards.every(card =>
        card.all(".board-card-top")[0].firstChild?.className.includes("board-kind")));
    const marks = cards.map(card => card.all(row => row.tagName === "PATH")[0]?.getAttribute("d"));
    check("six execution types use distinct icon silhouettes", marks.every(Boolean) && new Set(marks).size === 6);
    check("decorative icons do not duplicate spoken type labels", cards.every(card =>
        card.all(row => row.tagName === "SVG")[0]?.getAttribute("aria-hidden") === "true"));
    check("each type has its own visual treatment", kinds.every(type => css.includes('.board-kind-' + type + ' ')));
    p.view.leave();
}
{
    const p = page({ read: async () => envelope({ readState: { status: "stale", refreshing: false, error: { code: "source_failed" } }, source: { observedAt: 1700000000, ingestion: { issueCount: 1, reasons: ["graph_node_binding_conflict"] } } }) });
    await p.view.open("a");
    check("a failed refresh is not described as actively updating", p.elements["board-status"].textContent.includes("更新暫時失敗") && !p.elements["board-status"].textContent.includes("正在背景更新"));
    check("reading status explains coverage without raw protocol jargon", !p.elements["board-status"].textContent.includes("graph_node_binding_conflict") && p.elements["board-status"].textContent.includes("資料截至"));
    p.view.leave();
}
{
    const readable = item("Raw broker graph destination that nobody should need to parse", "execution");
    readable.summary = "SOURCE-OBJECTIVE-UNIQUE";
    readable.presentation = { authority: "narrative_only", variants: [
        { locale: "zh-TW", status: "current", title: "讓手機閱讀更清楚", summary: "改善手機上的文章排版。", outcome: "", nextStep: "" },
        { locale: "en", status: "current", title: "Readable mobile articles", summary: "Improve article layout on phones." }
    ] };
    readable.cardSummary = { checklist: { required: 5, requiredCompleted: 3, coverage: "complete" } };
    const p = page({ read: async () => envelope({ items: [readable], item: readable }) });
    await p.view.open("a");
    check("localized persisted AI title replaces raw destination on the card",
        p.elements["board-items"].all(".board-card-title")[0].textContent === "讓手機閱讀更清楚");
    check("compact card counts do not require loading checklist records",
        p.elements["board-items"].textContent.includes("3/5"));
    await p.view.open("a", readable.id);
    check("detail leads with the same readable title and identifies AI narrative",
        p.elements["board-detail"].all(".board-detail-title")[0].textContent === "讓手機閱讀更清楚"
        && p.elements["board-detail"].textContent.includes("AI 整理"));
    check("AI reading retains the original objective in the detail",
        p.elements["board-detail"].textContent.includes("SOURCE-OBJECTIVE-UNIQUE"));
    p.view.leave();
    const en = page({ read: async () => envelope({ items: [readable] }) }, "en");
    await en.view.open("a");
    check("configured reading language chooses the matching stored variant",
        en.elements["board-items"].all(".board-card-title")[0].textContent === "Readable mobile articles");
    en.view.leave();
}
{
    const pendingItem = item("Waiting for acceptance", "verified");
    pendingItem.progress = { state: "verified", group: "waiting", active: false };
    const p = page({ read: async () => envelope({ items: [pendingItem] }) });
    await p.view.open("a");
    check("waiting work never claims there is nothing left to advance",
        !p.elements["board-items"].textContent.includes("目前沒有待推進"));
    p.view.leave();
}
{
    const epic = item("Program", "execution"); epic.type = "epic";
    epic.deliveryLanes = [{ id: "cloud-child", projectId: "b", projectName: "Cloud", title: "接收排程通知", owner: "%406", progress: { state: "execution" }, checklistDone: 2, checklistTotal: 4 }];
    const reads = [];
    const p = page({ read: async (project, id) => { reads.push([project, id]); return envelope({ item: epic }); } });
    await p.view.open("a", epic.id);
    const lane = p.elements["board-detail"].all(".board-delivery-lane")[0];
    check("Epic presents related delivery with Project, owner and progress", lane && lane.textContent.includes("Cloud") && lane.textContent.includes("%406") && lane.textContent.includes("執行中"));
    lane.click(); await flush();
    check("cross-Project delivery opens the item's owning Project", reads.some(([project, id]) => project === "b" && id === "cloud-child"));
    p.view.leave();
}
{
    const declared = item("declared", "execution");
    declared.progress.basisCodes = ["declared_output_span"];
    const p = page({ read: async () => envelope({ items: [declared] }) });
    await p.view.open("a");
    check("declared activity is visibly attributed rather than presented as observed execution",
        p.elements["board-items"].textContent.includes("Session 回報")
        && p.elements["board-items"].textContent.includes("尚未取得派工系統的執行確認"));
    p.view.leave();
}
{
    const p = page({ read: () => Promise.resolve(envelope({
        projects: [{ id: "a", name: "Clawdline", summary: { active: 24, waiting: 6, landed: 90 } }],
        items: [], truncated: true
    })) });
    await p.view.open("a");
    check("Project totals come from its materialized summary, not loaded item rows",
        p.elements["board-items"].all(".board-overview-stat").map(row => row.textContent).join("|")
            === "24正在進行|6等待處理|90已落地");
    check("partial item projection does not contradict a nonempty Project model",
        !p.elements["board-items"].textContent.includes("目前沒有待推進的項目"));
    p.view.leave();
}
{
    const p = page({ read: async () => envelope({
        projects: [{ id: "a", name: "Clawdline", summary: { active: 0, waiting: 0, landed: 0, history: 50 } }],
        items: [], truncated: true
    }) });
    await p.view.open("a");
    check("omitted historical rows cannot be called no remaining work",
        !p.elements["board-items"].textContent.includes("目前沒有待推進"));
    check("omitted historical rows explicitly say they are not loaded",
        p.elements["board-items"].textContent.includes("歷史紀錄尚未載入"));
    p.view.leave();
}
{
    const historical = item("old-audit", "delivered");
    historical.progress = { state: "delivered", active: false, historical: true, group: "history" };
    const live = item("live", "execution");
    live.progress = { state: "execution", active: true, historical: false, group: "active" };
    const p = page({ read: async () => envelope({ items: [historical, live] }) });
    await p.view.open("a");
    const retained = p.elements["board-items"].all(".board-unconfirmed-history")[0];
    check("collapsed historical groups do not construct every hidden card", retained.all(".board-item-card").length === 0);
    retained.open = true; retained.dispatch("toggle");
    check("unfinished history stays readable outside current work without claiming completion",
        retained && retained.tagName === "DETAILS" && retained.textContent.includes("old-audit")
        && !retained.textContent.includes("live <literal>"));
    check("old audit cannot inflate active counter", p.elements["board-items"]
        .all(".board-overview-stat")[0].textContent === "1正在進行");
    p.view.leave();
}
{
    const reads = [],
        commands = [],
        modes = [];
    const p = page({
        read: (project, id) => {
            reads.push([project, id]);
            return Promise.resolve(
                envelope({
                    item: id ? rows.find((row) => row.id === id) : null
                })
            );
        },
        command: (body) => commands.push(body),
        onMode: (board) => modes.push(board.enabled)
    });
    await p.view.enter();
    check(
        "project index contains two project cards",
        p.elements["board-items"].all(".board-project-card").length === 2
    );
    check("index never mixes in work cards", p.elements["board-items"].all(".board-item-card").length === 0);
    p.elements["board-items"].all(".board-project-card")[0].click();
    await flush();
    check("project click reads its own identity", reads.at(-1)[0] === "a");
    check("header names selected Project", p.elements["board-title"].textContent === "Clawdline");
    check(
        "even unfiltered server data cannot mix projects",
        !p.elements["board-items"].textContent.includes("foreign")
    );
    check("current scoped items render without constructing collapsed history", p.elements["board-items"].all(".board-item-card").length === 2);
    check("payload is literal text", p.elements["board-items"].textContent.includes("<literal>"));
    const history = p.elements["board-items"].all(".board-history-group")[0];
    check("history is a collapsible region", history.tagName === "DETAILS");
    history.open = true; history.dispatch("toggle");
    check("landed and canceled retained in history", history.all(".board-item-card").length === 2);
    check("delivery alone stays outside completed history", !history.textContent.includes("delivered"));
    const stats = p.elements["board-items"].all(".board-overview-stat");
    check("canceled is excluded from landed count", stats[2].textContent === "1已落地");
    check("known lifecycle stages have a visual journey", p.elements["board-items"].all(".board-journey").length === 2);
    const deliveredCard = p.elements["board-items"].all(".board-item-card")
        .find(row => row.dataset.boardItemId === "delivered");
    check("delivery has an explicit visible checkpoint instead of four unlit stages",
        deliveredCard.all(".board-progress-checkpoint").length === 1
        && deliveredCard.all(".board-journey").length === 0
        && deliveredCard.textContent.includes("階段待確認"));
    check("canceled work does not look like an unstarted four-stage journey",
        p.elements["board-items"].all(".board-item-card")
            .find(row => row.dataset.boardItemId === "stopped").all(".board-journey").length === 0);
    check(
        "each journey has one observed stage at most",
        p.elements["board-items"]
            .all(".board-journey")
            .every((row) => row.all((n) => n.attributes["aria-current"] === "step").length <= 1)
    );
    p.elements["board-search"].value = "active";
    p.elements["board-search"].dispatch("input");
    check("search filters within project", p.elements["board-items"].all(".board-item-card").length === 1);
    p.elements["board-items"].all(".board-item-card")[0].click();
    await flush();
    check(
        "detail requests exact item and project",
        JSON.stringify(reads.at(-1)) === JSON.stringify(["a", "active"])
    );
    check(
        "detail never exposes edit, state or create controls",
        p.elements["board-detail"].all((n) => ["FORM", "SELECT", "INPUT"].includes(n.tagName)).length === 0
    );
    check("view issues zero mutations even with admin authority", commands.length === 0);
    check("mode is delivered to shell", modes.every(Boolean));
    await p.view.escape();
    check("back returns to project item list", p.view.state.view === "items");
    p.view.leave();
    check("leaving stops scheduled refresh", p.timers.size === 0);
}
{
    const detail = {
        ...rows[0],
        checklist: [{ title: "Keyboard works", status: "doing", required: true }],
        milestones: [{ title: "Mobile ready", status: "passed" }],
        obligations: [
            {
                title: "Resolve blocker",
                owner: "root",
                blocking: true,
                resolved: false
            }
        ],
        artifacts: [
            { title: "Safe result", url: "https://example.test/result", kind: "document" },
            { title: "Unsafe", url: "javascript:alert(1)" }
        ],
        documentReferences: [
            { id: "plan-v1", title: "Original Board plan", purpose: "plan",
              documentId: "board-plan", version: 1, status: "superseded",
              authority: "narrative_only",
              url: "https://app.clawdline.com/#document=1&machine=mac-a&session=session-a&scope=project&path=plan-v1.md" },
            { id: "plan-v2", title: "Current Board plan", purpose: "plan",
              documentId: "board-plan", version: 2, status: "current",
              authority: "narrative_only", supersedesId: "plan-v1",
              url: "https://app.clawdline.com/#document=1&machine=mac-a&session=session-a&scope=project&path=plan-v2.md" },
            { id: "decision-v1", title: "Approved interaction boundary", purpose: "decision",
              documentId: "interaction-decision", version: 1, status: "current",
              authority: "narrative_only",
              url: "https://app.clawdline.com/#document=1&machine=mac-a&session=session-a&scope=task&task=11111111-2222-4333-8444-555555555555&path=decision.md" }
        ],
        remainingWork: {
            work: [
                { kind: "checklist", title: "Keyboard works", status: "doing",
                  disposition: "required", owner: "root" },
                { kind: "checklist", title: "Optional polish", status: "todo",
                  disposition: "optional", owner: "root" },
                { kind: "child", title: "Canceled experiment", status: "canceled",
                  disposition: "canceled", owner: "root" },
                { kind: "obligation", title: "Unclassified older wait", status: "waiting",
                  actorKind: "unknown", disposition: "unknown", owner: "unknown" }
            ],
            userDecisions: [
                { kind: "obligation", title: "Choose AI policy", status: "waiting",
                  actorKind: "user", disposition: "required", owner: "user",
                  requiredAction: "Choose whether stored text may leave the Mac" }
            ],
            workCount: 5,
            userDecisionCount: 2,
            workOmittedCount: 1,
            userDecisionOmittedCount: 1
        },
        links: [
            {
                kind: "session",
                targetId: "conversation",
                label: "Build conversation"
            },
            { kind: "worktree", label: "delivery-branch" }
        ],
        history: [{ at: 1700000000, summary: "Execution began" }],
        landings: [
            {
                id: "historical",
                sourceId: "receipt-1",
                subject: "commit-1",
                summary: "Commit is contained"
            }
        ],
        currentEvidence: {},
        usage: {
            state: "partial",
            measured: 120,
            total: null,
            undeclaredRows: 1
        },
        projection: {
            history: { omittedCount: 99 },
            documentReferences: { retainedCount: 3, omittedCount: 1 }
        },
        historyDroppedCount: 101
    };
    const openedSessions = [], collectionReads = [];
    const p = page(
        {
            read: (_project, selector) => {
                collectionReads.push(selector);
                if (selector === "collection:active:remaining_work:4")
                    return Promise.resolve(envelope({ collection: {
                        kind: "remaining_work", itemId: "active", offset: 4,
                        rows: [{ kind: "obligation", id: "late-blocker",
                            title: "Late blocking proof", owner: "root", status: "waiting",
                            actorKind: "agent", blocking: true, disposition: "required" }],
                        totalCount: 5, nextOffset: null
                    }}));
                if (selector === "collection:active:user_decisions:1")
                    return Promise.resolve(envelope({ collection: {
                        kind: "user_decisions", itemId: "active", offset: 1,
                        rows: [{ kind: "obligation", id: "late-decision",
                            title: "Late user decision", owner: "user", status: "waiting",
                            actorKind: "user", blocking: true, disposition: "required" }],
                        totalCount: 2, nextOffset: null
                    }}));
                if (selector === "collection:active:document_references:3")
                    return Promise.resolve(envelope({ collection: {
                        kind: "document_references", itemId: "active", offset: 3,
                        rows: [{ id: "reference-late", title: "Late reference", purpose: "reference",
                            documentId: "late-reference", version: 1, status: "current",
                            authority: "narrative_only",
                            url: "https://app.clawdline.com/#document=1&machine=mac-a&session=session-a&scope=project&path=late.md" }],
                        totalCount: 4, nextOffset: null
                    }}));
                return Promise.resolve(envelope({
                    item: detail,
                    source: { ingestion: { issueCount: 1, reasons: ["link_capacity"] } }
                }));
            },
            openSession: (...args) => {
                openedSessions.push(args);
                return { error: "session_unavailable" };
            }
        },
        "en"
    );
    await p.view.open("a", "active");
    const target = p.elements["board-detail"];
    for (const value of [
        "Keyboard works",
        "Mobile ready",
        "Resolve blocker",
        "Execution began",
        "120",
        "Undeclared",
        "receipt-1",
        "commit-1",
        "Historical record",
        "99 records omitted",
        "2 older records"
    ])
        check("detail retains " + value, target.textContent.includes(value));
    check("first-screen remainder separates work from user decisions",
        target.textContent.includes("What we will do next")
        && target.textContent.includes("What you need to decide")
        && target.textContent.includes("Choose whether stored text may leave the Mac"));
    check("remaining rows preserve optional, canceled and unknown distinctions",
        target.textContent.includes("Optional") && target.textContent.includes("Canceled")
        && target.textContent.includes("Owner kind unknown"));
    check("typed documents are separated into plan and decision sections",
        target.textContent.includes("Original plan")
        && target.textContent.includes("Decisions & changes")
        && target.textContent.includes("Current Board plan")
        && target.textContent.includes("Approved interaction boundary"));
    const expansionButtons = target.all((n) => n.dataset.boardAction === "load-more");
    check("omitted remaining work, decisions and documents expose bounded controls",
        expansionButtons.length === 3);
    expansionButtons.forEach(node => node.click());
    await flush();
    check("bounded collection reads append every omitted class without replacing detail",
        target.textContent.includes("Late blocking proof")
        && target.textContent.includes("Late user decision")
        && target.textContent.includes("Late reference"));
    check("collection selectors preserve item, kind and current offset",
        ["collection:active:remaining_work:4", "collection:active:user_decisions:1",
         "collection:active:document_references:3"].every(value => collectionReads.includes(value)));
    check("superseded document metadata remains visible without loading a body",
        target.textContent.includes("Earlier version") && !target.textContent.includes("document body"));
    check("an old generic document artifact remains an output, not an approved plan",
        target.all(".board-document-reference").every(row => !row.textContent.includes("Safe result"))
        && target.all(".board-output-link").some(row => row.textContent.includes("Safe result")));
    check(
        "safe result has opener protection",
        target.all(".board-output-link")[0].rel === "noopener noreferrer"
    );
    check("unsafe scheme is not an output anchor",
        target.all(".board-output-link").filter(row => row.tagName === "A").length === 1);
    check("partial usage is not an exact total", target.textContent.includes("≥ 120"));
    check("source gaps remain visible with technical detail available",
        p.elements["board-status"].textContent.includes("source records still need matching")
        && p.elements["board-status"].title.includes("link_capacity"));
    target.all((n) => n.dataset.boardAction === "open-session")[0].click();
    await flush();
    check("Session navigation receives the selected Project presentation",
        openedSessions[0][0] === "conversation" && openedSessions[0][1].id === "a");
    check(
        "missing live session yields visible refusal",
        p.elements["board-status"].textContent.includes("no unique live Session")
    );
    p.view.leave();
}
{
    const old = deferred(),
        newest = deferred();
    const p = page({
        read: (_, id) => (id === "old" ? old.promise : newest.promise)
    });
    const first = p.view.open("a", "old"),
        second = p.view.open("a", "new");
    newest.resolve(envelope({ item: item("new", "verified") }));
    await second;
    old.resolve(envelope({ item: item("old", "execution") }));
    await first;
    check(
        "late old detail cannot repaint selection",
        p.elements["board-detail"].textContent.includes("new <literal>") &&
            !p.elements["board-detail"].textContent.includes("old <literal>")
    );
    p.view.leave();
}
{
    const pending = deferred();
    const p = page({ read: () => pending.promise });
    const opening = p.view.open("a", null, { id: "a", label: "Chosen project" });
    check("clicked Project identity appears before the network resolves",
        p.elements["board-title"].textContent === "Chosen project");
    check("initial loading is not a zero-count dashboard",
        p.elements["board-items"].all(".board-overview-stat").length === 0);
    check("initial loading never claims the Project has no work",
        !p.elements["board-items"].textContent.includes("No open work")
        && !p.elements["board-items"].textContent.includes("目前沒有待推進"));
    pending.resolve(envelope({ projects: [], items: [], readState: { status: "loading" } }));
    await opening;
    check("background initialization remains loading, not authoritative empty",
        p.elements["board-items"].all(".board-overview-stat").length === 0);
    check("a cold snapshot cannot erase the clicked Project name",
        p.elements["board-title"].textContent === "Chosen project");
    p.view.leave();
}
{
    const p = page({ read: () => Promise.resolve(envelope({ projects: [], items: [],
        readState: { status: "ready" } })) }, "en");
    await p.view.open("missing", null, { id: "missing", label: "Chosen project" });
    check("unknown Project keeps the identity the user selected",
        p.elements["board-title"].textContent === "Chosen project");
    check("a catalog shell cannot masquerade as an empty selected Project",
        p.elements["board-items"].all(".board-overview-stat").length === 0
        && !p.elements["board-items"].textContent.includes("No open work"));
    check("unknown Project is explicitly unavailable",
        p.elements["board-status"].textContent.includes("Project records are unavailable"));
    p.view.leave();
}
{
    let reads = 0;
    const hold = deferred();
    const p = page({
        read: () => {
            reads++;
            return reads === 2
                ? hold.promise
                : Promise.resolve(
                      envelope({
                          revision: reads,
                          items: [item("active", reads > 1 ? "landed" : "execution")]
                      })
                  );
        }
    });
    await p.view.open("a");
    check("one automatic refresh is scheduled", p.timers.size === 1);
    const tick = p.timers.values().next().value;
    p.timers.clear();
    tick();
    await flush();
    p.view.refresh();
    p.view.refresh();
    await flush();
    check("refresh shares in-flight read, preventing overlap", reads === 2);
    hold.resolve(envelope({ revision: 2, items: [item("active", "landed")] }));
    await flush();
    check(
        "new broker status appears without human transition",
        p.elements["board-items"].all(".board-history-group").length === 1
    );
    p.doc.hidden = true;
    const hiddenTick = p.timers.values().next().value;
    p.timers.clear();
    hiddenTick();
    await flush();
    check("background document does not keep polling server", reads === 2);
    p.view.leave();
    check("leaving tears down poll ownership", p.timers.size === 0);
}
{
    let fail = false;
    const p = page(
        {
            read: () => (fail ? Promise.reject(new Error("offline")) : Promise.resolve(envelope()))
        },
        "en"
    );
    await p.view.open("a");
    fail = true;
    await p.view.refresh();
    check(
        "failed refresh keeps last known records",
        p.elements["board-items"].all(".board-item-card").length === 2
    );
    check(
        "failure names stale information rather than empty success",
        p.elements["board-status"].textContent.includes("may be old")
    );
    p.view.leave();
}
{
    const retained = {
        ...item("off-report", "landed"),
        state: "closed",
        completionReport: {
            status: "current", version: 1, objective: "Retained while off",
            deliveredOutcomes: "Readable history", verificationLanding: "Separate evidence",
            remainingWork: "None recorded", lessons: "Off mode preserves reports",
            sourceReferences: []
        }
    };
    const p = page({ read: (_, id) => Promise.resolve(envelope({ enabled: false, item: id ? retained : null })) }, "en");
    await p.view.open("a", retained.id);
    check(
        "disabled mode retains read-only history",
        p.elements["board-status"].textContent.includes("read-only")
    );
    check("disabled mode keeps report history readable", p.elements["board-detail"].textContent.includes("Retained while off"));
    check("disabled mode stops automatic workflow refresh", p.timers.size === 0);
    p.view.leave();
}
check(
    "conversation resolves exactly one live terminal",
    resolveBoardSession([{ id: "%42", sessionId: "c" }], "c").id === "%42"
);
{
    const coord = {
        ...item("coordination", "landed"),
        type: "coordination",
        typeDetails: {
            outcomes: "Joined two delivery lines",
            difficulties: "Overlapping claims",
            improvements: "Declare scopes earlier"
        },
        completionReport: { status: "current", id: "coord-report", version: 1,
            objective: "Bound one coordination period", deliveredOutcomes: "Handoff prepared",
            verificationLanding: "Narrative only", remainingWork: "Receiver continues",
            lessons: "Name the boundary", reportBoundary: { kind: "handoff",
                label: "Root to receiver", handoffId: "handoff-1" }, sourceReferences: [] }
    };
    const p = page(
        {
            read: (_, id) => Promise.resolve(envelope({ items: [coord], item: id ? coord : null }))
        },
        "en"
    );
    await p.view.open("a");
    check("coordination never has lifecycle status", boardProgress(coord) === "not_applicable");
    check(
        "coordination lives in its own record group",
        p.elements["board-items"].all(".board-coordination-group").length === 1
    );
    check(
        "coordination never contributes to landed total",
        p.elements["board-items"].all(".board-overview-stat")[2].textContent === "0Landed"
    );
    check(
        "coordination has no visual lifecycle",
        p.elements["board-items"].all(".board-journey").length === 0
    );
    await p.view.open("a", coord.id);
    for (const value of ["Joined two delivery lines", "Overlapping claims", "Declare scopes earlier"])
        check(
            "coordination narrative retains " + value,
            p.elements["board-detail"].textContent.includes(value)
        );
    check(
        "coordination detail also omits lifecycle",
        p.elements["board-detail"].all(".board-state-pill").length === 0
    );
    check(
        "coordination uses period and handoff report wording",
        p.elements["board-detail"].textContent.includes("Period / handoff report") &&
            !p.elements["board-detail"].textContent.includes("Completion report")
    );
    check("coordination report displays its typed handoff boundary",
        p.elements["board-detail"].textContent.includes("Root to receiver · same-item handoff"));
    p.view.leave();
}
{
    const bug = {
        ...item("bug", "execution"),
        type: "bug",
        typeDetails: {
            rootCause: "A lost ownership boundary",
            lessons: "Compare the same record across both surfaces"
        }
    };
    const p = page({ read: () => Promise.resolve(envelope({ item: bug })) }, "en");
    await p.view.open("a", bug.id);
    check(
        "bug root cause is a named section",
        p.elements["board-detail"].textContent.includes("Why it happenedA lost ownership boundary")
    );
    check(
        "bug lessons are retained alongside result",
        p.elements["board-detail"].textContent.includes("Compare the same record across both surfaces")
    );
    p.view.leave();
}
{
    const reported = {
        ...item("reported", "delivered"),
        state: "closed",
        completionReport: {
            status: "current",
            version: 2,
            authoredAt: 1700000000,
            actor: "root-report",
            authorship: "assistant",
            model: "small-model",
            objective: "Make closure understandable.",
            deliveredOutcomes: "Rendered <img src=x onerror=alert(1)> as literal text.",
            verificationLanding: "Delivered only; no landing proof was forged.",
            remainingWork: "Run a separate historical pilot.",
            lessons: "Narrative is not evidence.",
            sourceReferences: [
                { kind: "artifact", targetId: "a", label: "Safe source", url: "https://example.test/a", resolution: "same_item", authority: "narrative_only", resolvedAt: 1700000000 },
                { kind: "external", targetId: "b", label: "Unsafe source", url: "javascript:alert(1)", resolution: "unresolved", authority: "narrative_only" }
            ]
        },
        completionReportHistory: [
            { id: "older-report", version: 1, status: "superseded", authoredAt: 1699990000,
              actor: "root-report", authorship: "assistant", sourceCount: 1 }
        ]
    };
    const oldReport = {
        id: "older-report", version: 1, status: "superseded", authoredAt: 1699990000,
        actor: "root-report", authorship: "assistant", objective: "Earlier objective body",
        deliveredOutcomes: "Earlier outcome body", verificationLanding: "Earlier proof limits",
        remainingWork: "Earlier remainder", lessons: "Earlier durable lesson",
        sourceReferences: [{ kind: "task", targetId: "old-task", label: "Old task",
            resolution: "same_item", authority: "narrative_only", resolvedAt: 1699990000 }]
    };
    const reads = [];
    const p = page({ read: (_project, id) => {
        reads.push(id);
        return Promise.resolve(id && id.startsWith("report:")
            ? envelope({ items: [], item: reported, reportSelection: oldReport })
            : envelope({ item: reported }));
    } }, "en");
    await p.view.open("a", reported.id);
    const target = p.elements["board-detail"];
    for (const value of [
        "Completion report",
        "Objective",
        "Delivered outcomes",
        "Verification & landing",
        "Remaining work",
        "Lessons",
        "Make closure understandable.",
        "Narrative is not evidence.",
        "Attributed narrative · not verification or landing evidence"
    ])
        check("completion report renders " + value, target.textContent.includes(value));
    check("report keeps markup literal", target.textContent.includes("<img src=x onerror=alert(1)>"));
    check("report markup cannot create executable elements", target.all((n) => ["IMG", "SCRIPT"].includes(n.tagName)).length === 0);
    check("only safe report source URLs become links", target.all((n) => n.tagName === "A").length === 1);
    check("safe report links isolate their opener", target.all((n) => n.tagName === "A")[0].rel === "noopener noreferrer");
    check("source relationship is qualified as authoring-time provenance",
        target.textContent.includes("Same item at authorship"));
    check("a report never promotes delivered-only progress", boardProgress(reported) === "delivered");
    const historyButton = target.all(".board-report-history-button")[0];
    check("superseded report metadata exposes a lazy reader", !!historyButton);
    historyButton.click();
    await flush();
    check("history expansion requests one opaque report with the selected item",
        reads[1] === "report:" + reported.id + ":older-report");
    check("a selected prior report body becomes readable without replacing the current body",
        target.textContent.includes("Earlier durable lesson")
            && target.textContent.includes("Narrative is not evidence."));
    p.view.leave();
}
{
    const current = {
        ...item("report-failure", "landed"), state: "closed",
        completionReport: { status: "current", id: "current-report", version: 2,
            objective: "Current body stays readable", deliveredOutcomes: "Current result",
            verificationLanding: "Current limits", remainingWork: "Current remainder",
            lessons: "Current lesson", sourceReferences: [] },
        completionReportHistory: [{ id: "prior-failure", version: 1, status: "superseded" }]
    };
    const p = page({ read: (_project, id) => id && id.startsWith("report:")
        ? Promise.reject(new Error("version offline"))
        : Promise.resolve(envelope({ item: current })) }, "en");
    await p.view.open("a", current.id);
    p.elements["board-detail"].all(".board-report-history-button")[0].click();
    await flush();
    check("report-version failure stays inside history reader",
        p.elements["board-detail"].textContent.includes("version offline"));
    check("report-version failure never clears the current report or item selection",
        p.elements["board-detail"].textContent.includes("Current body stays readable")
            && p.view.state.item.id === current.id && p.view.state.readStatus !== "error");
    p.view.leave();
}
{
    const hold = deferred(), current = {
        ...item("stale-report-item", "landed"), state: "closed",
        completionReport: { status: "current", id: "current-stale", version: 2,
            objective: "Current stale guard", deliveredOutcomes: "Current result",
            verificationLanding: "Current limits", remainingWork: "Current remainder",
            lessons: "Current lesson", sourceReferences: [] },
        completionReportHistory: [{ id: "prior-stale", version: 1, status: "superseded" }]
    }, next = { ...item("next-item", "execution"), completionReport: { status: "absent" } };
    const p = page({ read: (_project, id) => id && id.startsWith("report:")
        ? hold.promise : Promise.resolve(envelope({ item: id === next.id ? next : current })) }, "en");
    await p.view.open("a", current.id);
    p.elements["board-detail"].all(".board-report-history-button")[0].click();
    await p.view.open("a", next.id);
    hold.resolve(envelope({ items: [], item: current, reportSelection: {
        id: "prior-stale", version: 1, status: "superseded", objective: "Stale prior body",
        deliveredOutcomes: "old", verificationLanding: "old", remainingWork: "old",
        lessons: "old", sourceReferences: [] } }));
    await flush();
    check("late report response cannot overwrite a newer item selection",
        p.view.state.item.id === next.id
            && !p.elements["board-detail"].textContent.includes("Stale prior body"));
    p.view.leave();
}
{
    const absent = { ...item("absent", "execution"), completionReport: { status: "absent" } };
    const p = page({ read: () => Promise.resolve(envelope({ item: absent })) }, "en");
    await p.view.open("a", absent.id);
    check("missing report is stated honestly", p.elements["board-detail"].textContent.includes("No completion report has been recorded"));
    p.view.leave();
}
{
    const historical = {
        ...item("historical-report", "execution"),
        completionReport: {
            status: "historical_needs_update",
            version: 1,
            objective: "Earlier objective",
            deliveredOutcomes: "Earlier delivery",
            verificationLanding: "Earlier receipts",
            remainingWork: "New scope",
            lessons: "Earlier lesson",
            sourceReferences: []
        }
    };
    const p = page({ read: () => Promise.resolve(envelope({ item: historical })) }, "en");
    await p.view.open("a", historical.id);
    check("reopened scope visibly qualifies its old report", p.elements["board-detail"].textContent.includes("Earlier scope · needs an updated report"));
    p.view.leave();
}
check(
    "absent conversation never becomes guessed terminal",
    resolveBoardSession([], "c").error === "session_unavailable"
);
check(
    "ambiguous conversation is refused",
    resolveBoardSession(
        [
            { id: "%42", sessionId: "c" },
            { id: "%43", sessionId: "c" }
        ],
        "c"
    ).error === "session_ambiguous"
);
console.log(`${checks} web board behavioral checks passed`);
