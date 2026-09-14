import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { bindBoardPage, boardProgress, resolveBoardSession } from "../Resources/web/app/js/view/board.js";
import * as boardModule from "../Resources/web/app/js/view/board.js";
import {
    boardWorkflowRecordHTML,
    parseBoardWorkflowRecord
} from "../Resources/web/app/js/view/board-workflow-record.js";
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
    const timerDelays = [];
    let next = 0;
    const environment = {
        document: doc,
        read: () => Promise.resolve(envelope()),
        setTimeout: (fn, ms) => {
            timerDelays.push(ms);
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
        timerDelays,
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
    const parent = item("Human release", "execution");
    parent.key = "CLA-1";
    parent.listSummary = { coverage: "complete", group: "active", view: {
        audience: "human", role: "primary_work", defaultVisible: true, reasonCodes: ["explicit_work_item"]
    } };
    const child = item("Human acceptance", "execution");
    child.key = "CLA-2";
    child.listSummary = { coverage: "complete", group: "active", view: {
        audience: "human", role: "subtask", parentId: parent.id, defaultVisible: true,
        reasonCodes: ["catalog_reconciled"]
    } };
    const review = item("Agent review attempt", "review_testing");
    review.listSummary = { coverage: "complete", group: "history", view: {
        audience: "agent", role: "execution_record", defaultVisible: false,
        reasonCodes: ["inferred_broker_record"]
    } };
    const provenance = item("Transferred correction provenance", "landed");
    provenance.listSummary = { coverage: "complete", group: "completed", view: {
        audience: "agent", role: "provenance_record", defaultVisible: false,
        reasonCodes: ["inferred_provenance"]
    } };
    const archived = item("Obsolete planning residue", "planning");
    archived.listSummary = { coverage: "complete", group: "planning", view: {
        audience: "archive", role: "provenance_record", defaultVisible: false,
        reasonCodes: ["catalog_reconciled"]
    } };
    const project = { id: "a", name: "Clawdline", itemCount: 5, humanItemCount: 2,
        currentHumanItemCount: 2, agentRecordCount: 2,
        summaryCoverage: { status: "complete" }, summary: {
            active: 1, waiting: 0, landed: 1,
            humanListGroups: { active: 2, planning: 0, waiting: 0, history: 0,
                completed: 0, canceled: 0, coordination: 0 },
            agentListGroups: { active: 0, planning: 0, waiting: 0, history: 1,
                completed: 1, canceled: 0, coordination: 0 }
        } };
    const p = page({ read: async () => envelope({
        projects: [project], items: [parent, child, review, provenance, archived]
    }) });
    await p.view.open("a");
    const visible = p.elements["board-items"].all(".board-card-title").map(node => node.textContent);
    check("default Board list contains only concrete human work",
        visible.some(title => title.startsWith("Human release"))
            && !visible.some(title => title.startsWith("Human acceptance"))
            && !visible.some(title => title.startsWith("Agent review attempt"))
            && !visible.some(title => title.startsWith("Transferred correction provenance"))
            && !visible.some(title => title.startsWith("Obsolete planning residue")));
    check("human overview counts exclude retained agent records",
        p.elements["board-items"].all(".board-overview-stat")[0]?.textContent === "2正在進行");
    const childGroup = p.elements["board-items"].all(node =>
        node.dataset.boardSubtasksFor === parent.id)[0];
    check("subtasks are grouped under their exact parent and collapsed by default",
        childGroup?.tagName === "DETAILS" && !childGroup.open
            && childGroup.textContent.includes("子項目")
            && childGroup.textContent.includes("CLA-1")
            && !childGroup.all(".board-item-card").length);
    childGroup.open = true; childGroup.dispatch("toggle");
    const childCard = childGroup.all(node => node.dataset.boardItemId === child.id)[0];
    check("expanded subtasks are visually compact and clearly marked",
        childCard?.className.includes("board-subtask-card")
            && childCard.textContent.includes("子項目")
            && childCard.textContent.includes("CLA-1"));
    check("agent execution does not occupy the ordinary human board",
        !p.elements["board-items"].all(node =>
            node.dataset.boardSection === "agent-execution-details").length);
    p.elements["board-search"].value = "Agent review";
    p.elements["board-search"].dispatch("input");
    const searched = p.elements["board-items"].all(node =>
        node.dataset.boardSection === "agent-execution-details")[0];
    check("search can still find agent detail explicitly", searched?.open
        && searched.textContent.includes("Agent review attempt"));
    p.elements["board-search"].value = "Obsolete planning";
    p.elements["board-search"].dispatch("input");
    const searchedArchive = p.elements["board-items"].all(node =>
        node.dataset.boardSection === "archived-catalog-records")[0];
    check("exact search can recover an archived catalog record for audit",
        searchedArchive?.textContent.includes("Obsolete planning residue"));
    p.elements["board-search"].value = "Human acceptance";
    p.elements["board-search"].dispatch("input");
    const searchedSubtasks = p.elements["board-items"].all(node =>
        node.dataset.boardSubtasksFor === parent.id)[0];
    check("search opens the matching parent's subtask group",
        searchedSubtasks?.open && searchedSubtasks.textContent.includes("Human acceptance"));
    p.view.leave();
}
{
    const first = item("buried-agent", "landed"); first.title = "Buried review receipt";
    first.listSummary = { view: { audience: "agent", role: "execution_record" } };
    const second = item("buried-archive", "landed"); second.title = "Buried archived result";
    second.listSummary = { view: { audience: "archive", role: "provenance_record" } };
    const selectors = [];
    const p = page({ read: async (_project, selector) => {
        selectors.push(selector || "ordinary");
        if (!selector) return envelope({ items: [item("visible", "execution")], truncated: true });
        const offset = Number(selector.split(":")[2]);
        return envelope({ projects: [], items: [offset ? second : first], truncated: !offset,
            catalogSearch: { query: "buried", revision: 1, offset, totalCount: 2,
                nextOffset: offset ? null : 1 } });
    } });
    await p.view.open("a");
    p.elements["board-search"].value = "Buried";
    p.elements["board-search"].dispatch("input"); await flush();
    check("truncated Board search asks the server for the complete durable catalog",
        selectors.includes("catalog:1:0:buried")
            && p.elements["board-items"].textContent.includes("Buried review receipt"));
    const more = p.elements["board-items"].all(node =>
        node.dataset.boardAction === "catalog-search-more")[0];
    more.click(); await flush();
    check("catalog search continuation retains earlier matches and loads archived rows",
        selectors.includes("catalog:1:1:buried")
            && p.elements["board-items"].textContent.includes("Buried review receipt")
            && p.elements["board-items"].textContent.includes("Buried archived result"));
    p.view.leave();
}
{
    const planned = item("Future phase", "planning"); planned.parentId = "program";
    const livePlan = item("Live discovery", "planning");
    livePlan.progress = { state: "planning", group: "active", active: true };
    const queued = item("Ready now", "queued");
    const attention = item("Decision needed", "planning");
    attention.obligations = [{ blocking: true, resolved: false, actorKind: "user" }];
    const reviewDebt = item("Blocked historical review", "blocked");
    reviewDebt.progress = { state: "blocked", group: "history", evidenceCounts: { blockingFindings: 1 } };
    const failed = item("Failed verification", "planning");
    failed.progress = { state: "planning", group: "history", evidenceCounts: { failedVerifications: 1 } };
    const blocked = item("Explicit blocked", "blocked");
    blocked.progress = { state: "blocked", group: "history" };
    const finding = item("Open finding", "planning");
    finding.progress = { state: "planning", group: "waiting", evidenceCounts: { blockingFindings: 1 } };
    const original = JSON.stringify([reviewDebt, failed, blocked, finding]);
    const p = page({ read: async () => envelope({ items: [planned, livePlan, queued, attention, reviewDebt, failed, blocked, finding] }) });
    await p.view.open("a");
    const area = p.elements["board-items"].all(node => node.dataset.boardSection === "planned-work")[0];
    check("unstarted planning has an independent collapsed region", area && area.tagName === "DETAILS" && !area.open);
    check("planning region names its count without mounting future cards", area.textContent.includes("1") && !area.all(".board-item-card").length);
    const visible = p.elements["board-items"].all(".board-card-title").map(node => node.textContent).join("|");
    check("active planning, queued work and required attention remain visible", visible.includes("Live discovery") && visible.includes("Ready now") && visible.includes("Decision needed") && !visible.includes("Future phase"));
    check("historical blocked work and verification debt stay expanded", ["Blocked historical review", "Failed verification", "Explicit blocked", "Open finding"].every(title => visible.includes(title)));
    check("attention partition does not duplicate collapsed history or mutate authority", !p.elements["board-items"].all(".board-unconfirmed-history").length && JSON.stringify([reviewDebt, failed, blocked, finding]) === original);
    area.open = true; area.dispatch("toggle");
    const plannedChildren = area.all(node => node.dataset.boardSubtasksFor === "program")[0];
    check("expanding plans keeps nested subtasks collapsed and names their parent boundary",
        plannedChildren?.tagName === "DETAILS" && !plannedChildren.open
            && plannedChildren.textContent.includes("子項目") && planned.state === "backlog");
    plannedChildren.open = true; plannedChildren.dispatch("toggle");
    check("a second explicit expansion exposes the future subtask without changing lifecycle",
        plannedChildren.textContent.includes("Future phase") && planned.state === "backlog");
    p.elements["board-search"].value = "Future phase"; p.elements["board-search"].dispatch("input");
    const searched = p.elements["board-items"].all(node => node.dataset.boardSection === "planned-work")[0];
    check("search makes matching planned work discoverable", searched?.open && searched.textContent.includes("Future phase"));
    p.view.leave();
}
{
    // Real compact cards omit obligations/remainingWork; only the bounded model facts survive.
    const compact = (title, group, attention = {}) => ({ id: title, projectId: "a", title,
        type: "feature", state: "backlog", progress: { state: "planning", group: "waiting", active: false },
        listSummary: { coverage: "complete", group, attention } });
    const planned = compact("Future compact", "planning");
    const blocked = compact("Late blocker compact", "waiting", { blockingObligations: 1 });
    const decision = compact("User decision compact", "waiting", { userDecisions: 1 });
    const legacy = { ...compact("Unknown legacy", "planning") }; delete legacy.listSummary;
    const p = page({ read: async () => envelope({ items: [planned, blocked, decision, legacy], truncated: true,
        projects: [{ id: "a", name: "Clawdline", itemCount: 20, summaryCoverage: { status: "complete" },
            summary: { active: 0, waiting: 20, landed: 0,
                listGroups: { active: 0, planning: 17, waiting: 3, history: 0, completed: 0, canceled: 0, coordination: 0 } } }] }) });
    await p.view.open("a");
    const visible = p.elements["board-items"].all(".board-card-title").map(x => x.textContent).join("|");
    check("real compact blocking and user decisions remain visible", visible.includes("Late blocker compact") && visible.includes("User decision compact"));
    check("missing compact facts cannot silently certify a legacy item as pure planning", visible.includes("Unknown legacy"));
    check("pure compact plan is not mixed into attention", !visible.includes("Future compact"));
    const stats = p.elements["board-items"].all(".board-overview-stat").map(x => x.textContent).join("|");
    check("materialized planning total is separate from actual waiting", stats.includes("17規劃・尚未開始") && stats.includes("3等待推進／需要處理"));
    check("bounded list states loaded coverage rather than presenting it as the total", p.elements["board-items"].textContent.includes("已載入 4 / 20"));
    p.elements["board-search"].value = "Future compact"; p.elements["board-search"].dispatch("input");
    check("search states its loaded scope while complete-catalog search is pending", p.elements["board-items"].textContent.includes("已載入資料中有 1 項符合") && p.elements["board-items"].textContent.includes("17規劃・尚未開始"));
    p.view.leave();
}
{
    const choice = item("Legacy optional choice", "planning");
    choice.obligations = [{ actorKind: "user", blocking: false, resolved: false }];
    choice.projection = { obligations: { omittedCount: 0 } };
    const resolved = item("Resolved choice", "planning");
    resolved.obligations = [{ actorKind: "user", blocking: false, resolved: true }];
    const unknown = item("Unknown actor", "planning");
    unknown.obligations = [{ blocking: false, resolved: false }];
    const p = page({ read: async () => envelope({ items: [choice, resolved, unknown] }) });
    await p.view.open("a");
    const visible = p.elements["board-items"].all(".board-card-title").map(x => x.textContent).join("|");
    check("F1 legacy nonblocking explicit user choice stays visible without remainingWork", visible.includes("Legacy optional choice"));
    check("F1 resolved and unknown-actor nonblocking obligations do not invent user decisions", !visible.includes("Resolved choice") && !visible.includes("Unknown actor"));
    p.view.leave();
}
{
    const epic = item("Program", "execution"); epic.type = "epic";
    epic.progress.measurement = {
        status: "available", authority: "recorded_evidence", recordedPercent: 60,
        denominator: 5, completed: 3, leafCount: 2, unmeasuredLeafCount: 0,
        measuredAt: 1700000000, scope: "epic_leaf_acceptance",
        stages: {
            planning: { percent: 100, completed: 2, total: 2 },
            implementation: { percent: 80, completed: 4, total: 5 },
            verification: { percent: 60, completed: 3, total: 5 },
            release: { percent: null, completed: null, total: 2, status: "insufficient_evidence" }
        }
    };
    epic.presentation = { authority: "narrative_only", variants: [{ locale: "zh-TW", status: "current",
        title: "Program", summary: "", progressEstimate: { percent: 55, lowerBound: 40,
            upperBound: 70, confidence: "low", scope: "目前固定範圍",
            basis: "依目前範圍與交付敘述估計", authoredAt: 1700000001, model: "test" } }] };
    epic.deliveryLanes = [{ id: "cloud-child", projectId: "b", projectName: "Cloud", title: "接收排程通知", owner: "%406", progress: { state: "execution" }, checklistDone: 2, checklistTotal: 4 }];
    const reads = [];
    const p = page({ read: async (project, id) => { reads.push([project, id]); return envelope({ item: epic }); } });
    await p.view.open("a", epic.id);
    const progress = p.elements["board-detail"].all(".board-large-progress")[0];
    check("large work shows recorded evidence percentage", progress && progress.textContent.includes("60%") && progress.textContent.includes("實際驗收紀錄"));
    const progressbar = progress.all(node => node.getAttribute("role") === "progressbar")[0];
    check("recorded progressbar has a localized accessible name", progressbar && progressbar.getAttribute("aria-label") === "實際驗收紀錄");
    check("planning implementation verification and release remain separate", progress.textContent.includes("規劃 100%") && progress.textContent.includes("實作 80%") && progress.textContent.includes("驗收 60%") && progress.textContent.includes("發布 —"));
    check("AI estimate is visibly separate with its own confidence and provenance", progress.textContent.includes("AI 估計 55%") && progress.textContent.includes("40–70%") && progress.textContent.includes("信心 低") && progress.textContent.includes("test") && progress.textContent.includes("不會改變驗收或完成狀態"));
    const lane = p.elements["board-detail"].all(".board-delivery-lane")[0];
    check("Epic presents related delivery with Project, owner and progress", lane && lane.textContent.includes("Cloud") && lane.textContent.includes("%406") && lane.textContent.includes("執行中"));
    lane.click(); await flush();
    check("cross-Project delivery opens the item's owning Project", reads.some(([project, id]) => project === "b" && id === "cloud-child"));
    p.view.leave();
}
{
    const epic = item("Unscoped program", "execution"); epic.type = "epic";
    epic.progress.measurement = { status: "insufficient_scope", authority: "recorded_evidence",
        recordedPercent: null, denominator: 0, leafCount: 3, unmeasuredLeafCount: 2,
        measuredAt: 1700000000, scope: "epic_leaf_acceptance", stages: {} };
    epic.progress.lifecycleEstimate = { authority: "lifecycle_projection", percent: 45,
        lowerBound: 25, upperBound: 65, confidence: "low", scope: "epic_members",
        sampleCount: 3, measuredAt: 1700000000,
        basisCodes: ["bounded_phase_weights", "not_acceptance_evidence", "not_release_evidence"] };
    const p = page({ read: async (_project, id) => envelope(id ? { item: epic } : { items: [epic] }) });
    await p.view.open("a");
    const compact = p.elements["board-items"].all(".board-progress-compact")[0];
    check("large cards show the qualified estimate instead of universal denominator debt",
        compact && compact.textContent.includes("45%") && compact.textContent.includes("階段粗估")
        && !compact.textContent.includes("進度分母待補"));
    await p.view.open("a", epic.id);
    const progress = p.elements["board-detail"].all(".board-large-progress")[0];
    check("missing leaf scope is explicit instead of a guessed zero", progress && progress.textContent.includes("目前無法精確計算") && !progress.textContent.includes("0% 完成"));
    check("missing acceptance scope still shows a visibly qualified lifecycle estimate",
        progress && progress.textContent.includes("階段粗估 45%")
        && progress.textContent.includes("25–65%")
        && progress.textContent.includes("不代表驗收或發布"));
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
            === "24正在進行|6等待／規劃|90已落地");
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
        p.elements["board-status"].textContent.includes("no unique Session")
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
    // Said by its code, never by the message it was thrown with (`core/failure-text.js`).
    check("report-version failure stays inside history reader",
        p.elements["board-detail"].textContent.includes("The report could not be read.")
        && p.elements["board-detail"].textContent.includes("unexpected_error")
        && !p.elements["board-detail"].textContent.includes("version offline"));
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
{
    const project = "project-0123456789abcdef01234567";
    const itemId = "11111111-2222-4333-8444-555555555555";
    const url = boardModule.boardShareURL("mac with space", project, itemId);
    check("Board share URL fixes Cloud origin and machine/project/item identity",
        url === "https://app.clawdline.com/#page=board&machine=mac+with+space&project="
            + project + "&item=" + itemId);
    check("Board locator survives reload with the same exact identity",
        JSON.stringify(boardModule.boardLocatorFromHash(new URL(url).hash))
            === JSON.stringify({ machine: "mac with space", project, item: itemId }));
    check("unknown fields, duplicate identity keys and malformed percent encoding fail closed",
        boardModule.boardLocatorFromHash("#page=board&machine=mac&project=" + project
            + "&item=" + itemId + "&other=x") === null
        && boardModule.boardLocatorFromHash("#page=board&machine=mac&machine=other&project="
            + project) === null
        && boardModule.boardLocatorFromHash("#page=board&machine=%E0%A4%A&project="
            + project) === null
        && boardModule.boardLocatorFromHash("#page=board&machine=bad%&project="
            + project) === null);
    check("the local placeholder is never published as a Cloud machine identity",
        boardModule.boardShareURL("this-mac", project, itemId) === null);
    check("an invalid item cannot fall back to another Board item",
        boardModule.boardLocatorFromHash("#page=board&machine=mac&project=" + project
            + "&item=not-an-item") === null);

    const shareItem = item(itemId, "execution", project);
    const reads = [], copies = [], locations = [];
    const p = page({
        read: async (...args) => {
            reads.push(args);
            return envelope({ projects: [{ id: project, name: "Clawdline", itemCount: 1 }],
                items: [shareItem], item: args[1] ? shareItem : null });
        },
        copy: async value => { copies.push(value); },
        replaceURL: value => { locations.push(value); }
    });
    await p.view.openLocator({ project, item: itemId, machine: "mac with space" });
    check("explicit Board machine reaches the transport without entering the URL as a token",
        reads.length === 1 && reads[0][2] === "mac with space" && copies.length === 0);
    p.elements["board-detail"].all(node => node.dataset.boardAction === "copy-item-link")[0].click();
    await flush();
    check("copy is user-click-only and retains exact machine/project/item identity",
        copies.length === 1 && copies[0] === url);
    p.elements["board-back"].click();
    await flush();
    check("Back keeps the exact machine while returning from an item to its Project",
        reads.length === 2 && reads[1][0] === project && reads[1][1] === undefined
            && reads[1][2] === "mac with space");
    check("Back rewrites a shared item URL to the exact project locator used by reload",
        locations.length === 1
            && JSON.stringify(boardModule.boardLocatorFromHash(new URL(locations[0]).hash))
                === JSON.stringify({ machine: "mac with space", project, item: null }));
    p.view.leave();

    let refusedReads = 0;
    const refused = page({ read: async () => { refusedReads++; return envelope(); } });
    await refused.view.openLocator(null, "unsupported field");
    check("malformed reload stays an explicit error and never reads another Board item",
        refusedReads === 0 && refused.elements["board-status"].textContent.includes("無效")
            && !refused.elements["board-status"].all(node => node.dataset.boardAction === "retry").length);
    refused.view.leave();
}
{
    const conversation = "11111111-1111-4111-8111-111111111111";
    const grouped = boardModule.boardSessionGroups({ owner: "%458", links: [
        { id: "link-a", kind: "session", targetId: conversation, label: "first title" },
        { id: "link-b", kind: "session", targetId: conversation.toUpperCase(), label: "renamed" }
    ] }, [{ id: "%458", sessionId: conversation }]);
    check("same conversation is deduplicated by identity rather than title",
        grouped.conversationCount === 1 && grouped.receiptCount === 2
            && grouped.owner.length === 1 && grouped.owner[0].receipts.length === 2);
    const ambiguous = boardModule.boardSessionGroups({ owner: conversation, links: [
        { id: "one", kind: "session", targetId: conversation }
    ] }, [{ id: "%1", sessionId: conversation }, { id: "%2", sessionId: conversation }]);
    check("multiple live Sessions stay explicitly ambiguous",
        ambiguous.ambiguous.length === 1 && ambiguous.owner[0].activity === "ambiguous");
    const missingOwner = boardModule.boardSessionGroups({ owner: "%owner", links: [
        { id: "participant", kind: "session", targetId: conversation }
    ] }, [{ id: "%other", sessionId: conversation }]);
    check("another live participant does not become the recorded owner",
        missingOwner.ownerMissing && missingOwner.owner.length === 0
            && missingOwner.participants.length === 1);
    const otherConversation = "22222222-2222-4222-8222-222222222222";
    const fleet = boardModule.boardSessionGroups({ owner: "%same", links: [
        { id: "mac-a", kind: "session", targetId: conversation },
        { id: "mac-b", kind: "session", targetId: otherConversation }
    ] }, [
        { id: "%same", identity: { machine: "mac-a" }, sessionId: conversation },
        { id: "%same", identity: { machine: "mac-b" }, sessionId: otherConversation }
    ], "mac-a");
    check("terminal owner identity and live matching stay inside the selected Board machine",
        fleet.owner.length === 1 && fleet.owner[0].conversationId === conversation
            && fleet.participants.length === 0 && fleet.history.length === 1);
}
{
    const scope = "workflow_run=run-0123456789abcdef0123456789abcdef;session=%458;disposition=required";
    const relation = "wfs-" + "a".repeat(64);
    const exact = boardModule.mergeBoardRemainingWork({ remainingWork: { work: [
        { id: "check-1", kind: "checklist", title: "手機驗收", status: "todo",
          disposition: "required", owner: "%458", supplementRelationId: relation },
        { id: "owe-1", kind: "obligation", title: "手機驗收", status: "waiting",
          disposition: "required", owner: "%458", actorKind: "agent",
          requiredAction: "使用手機確認", blockingScope: scope,
          supplementRelationId: relation }
    ], userDecisions: [{ id: "decision", kind: "obligation", title: "選擇方案",
        actorKind: "user", requiredAction: "由使用者決定" }] } });
    check("one supplement checklist and obligation become one readable row with both sources",
        exact.work.length === 1 && exact.work[0].sources.length === 2
            && exact.work[0].requiredAction === "使用手機確認");
    check("user decisions are never consumed by supplement folding",
        exact.userDecisions.length === 1 && exact.userDecisions[0].requiredAction === "由使用者決定");
    const sameName = boardModule.mergeBoardRemainingWork({ remainingWork: { work: [
        { id: "check-a", kind: "checklist", title: "同名", disposition: "required" },
        { id: "check-b", kind: "checklist", title: "同名", disposition: "required" },
        { id: "owe-a", kind: "obligation", title: "同名", owner: "%458",
          actorKind: "agent", blockingScope: scope }
    ], userDecisions: [] } });
    check("same titles without unique supplement identity never merge",
        sameName.work.length === 3 && sameName.work.every(row => !row.sources));
    const differentSource = boardModule.mergeBoardRemainingWork({ remainingWork: { work: [
        { id: "manual", kind: "checklist", title: "同名", disposition: "required",
          supplementRelationId: "wfs-" + "b".repeat(64) },
        { id: "managed", kind: "obligation", title: "同名", disposition: "required",
          owner: "%458", actorKind: "agent", blockingScope: scope,
          supplementRelationId: "wfs-" + "c".repeat(64) }
    ], userDecisions: [] } });
    check("one same-title pair with different exact source identity stays as two rows",
        differentSource.work.length === 2 && differentSource.work.every(row => !row.sources));
    const legacy = boardModule.mergeBoardRemainingWork({
        checklist: [{ id: "todo", title: "保留待驗收", status: "doing", required: true },
            { id: "done", title: "完成項", status: "passed", required: true }],
        obligations: [{ id: "ask", title: "使用者選擇", resolved: false, blocking: true,
            actorKind: "user", requiredAction: "決定公開範圍" }]
    });
    check("older details still expose pending checks separately from completed checks",
        legacy.work.length === 1 && legacy.work[0].id === "todo"
            && legacy.userDecisions.length === 1 && legacy.userDecisions[0].id === "ask");
}

const workflowMetadata = {
    authority: "clawdline_metadata_not_user_authorization",
    board_epoch: 7,
    content_reference: "terminal-request:0123456789abcdef01234567",
    conversation_id: "11111111-1111-4111-8111-111111111111",
    coverage: "managed_ingress",
    helper: "clawdline-board-workflow <conversation-id> <stable-idempotency-key>",
    input_kind: "text_and_image",
    mode_gap: null,
    process_generation: "process-7",
    project_id: "project-0123456789abcdef01234567",
    provider: "codex",
    required_first_action: "begin",
    run_id: "run-0123456789abcdef0123456789abcdef",
    terminal_id: "%458",
    version: 1
};
const workflowWire = "<clawdline-workflow version=\"1\" authority=\"metadata-not-user\">\n"
    + JSON.stringify(workflowMetadata) + "\n</clawdline-workflow>";
const workflowImage = "<clawdline-image id=\"46cb6d40-c13f-4fea-9cf0-936f86b78da4\">";
{
    const withPath = value => workflowWire.replace(JSON.stringify(workflowMetadata),
        JSON.stringify({ ...workflowMetadata, helper_path: value }));
    check("installed helper absolute path remains folded without PATH lookup",
        parseBoardWorkflowRecord(withPath("/Applications/A relocated.app/Contents/Resources/clawdline-board-workflow"), "user") !== null);
    for (const value of ["clawdline-board-workflow", "/tmp/other", "/tmp/../clawdline-board-workflow",
        "/tmp/\nclawdline-board-workflow", null, "/" + "a".repeat(4096) + "/clawdline-board-workflow"]) {
        check("invalid helper path fails visible: " + JSON.stringify(value).slice(0, 60),
            parseBoardWorkflowRecord(withPath(value), "user") === null);
    }
}
{
    const template = {
        classification: "existing_item|new_work|question|clarification",
        item_id: "<existing_item>", operation: "begin", phase: "output",
        run_id: workflowMetadata.run_id, title: "<new_work>",
        type: "<new_work:task|feature|bug|refactor|coordination|epic>"
    };
    const item = "4f1c2d3e-5a6b-4c7d-8e9f-0a1b2c3d4e5f";
    const open = "<clawdline-workflow version=\"1\" authority=\"metadata-not-user\">\n";
    const close = "\n</clawdline-workflow>";
    const folds = changes => parseBoardWorkflowRecord(
        open + JSON.stringify({ ...workflowMetadata, ...changes }) + close, "user") !== null;
    check("legacy envelope without begin assistance still folds", folds({}));
    check("envelope carrying only the begin template folds", folds({ begin_template: template }));
    check("envelope carrying the template and a prior item folds",
        folds({ begin_template: template, previous_item: item }));
    // The exact line `ProjectBoardWorkflowPresentationTests.swift` requires the Swift producer to emit.
    const producer = readFileSync(new URL("./board-workflow-metadata-v1.json", import.meta.url),
        "utf8").trim();
    const produced = parseBoardWorkflowRecord("turn fixture-send-2\n\n" + open + producer + close, "user");
    check("the Swift producer's exact hinted metadata line folds in the web reader",
        produced?.raw === producer && produced?.text === "turn fixture-send-2"
            && produced?.metadata.previous_item === item);
    // A later producer may reword placeholders, add a field and drop one; its envelopes must keep
    // folding after that producer is gone. This is the first check a text-equality decoder fails.
    const { title: _omitted, ...missingField } = template;
    const future = { ...missingField, classification: "<existing_item|new_work|question|clarification>",
        item_id: "<exact item uuid>", type: "<task|feature|bug|refactor|coordination|epic>",
        handoff_id: "<handoff>" };
    check("future-shaped template with other placeholders, an extra key and no title folds",
        folds({ begin_template: future, previous_item: item }));
    // Valid under the structural contract; each was malformed while the decoder demanded the
    // producer's exact text.
    for (const [name, shape] of [
        ["missing an optional field", missingField],
        ["with an extra field", { ...template, summary: "added" }],
        ["whose choice was already made", { ...template, classification: "existing_item" }]
    ]) {
        check("template " + name + " folds", folds({ begin_template: shape }));
    }
    const widest = { operation: "begin", run_id: workflowMetadata.run_id };
    for (let length = 1; length <= 13; length++) widest["k_" + "a".repeat(length)] = `<${length}>`;
    widest["z".repeat(64)] = "界".repeat(85) + "a";
    check("template at the bounds folds: 16 keys, a 64-byte key, a 256-byte value",
        Object.keys(widest).length === 16 && folds({ begin_template: widest }));
    check("template of only operation and run_id folds",
        folds({ begin_template: { operation: "begin", run_id: workflowMetadata.run_id } }));
    check("unknown key beside begin assistance fails visible",
        !folds({ begin_template: template, previous_item: item, previous_item_title: "Fix it" }));
    const { operation: _operation, ...noOperation } = template;
    for (const [name, changes] of [
        ["uppercase prior item", { begin_template: template, previous_item: item.toUpperCase() }],
        ["human key as prior item", { begin_template: template, previous_item: "CLA-395" }],
        ["numeric prior item", { begin_template: template, previous_item: 7 }],
        ["null prior item", { begin_template: template, previous_item: null }],
        ["empty prior item", { begin_template: template, previous_item: "" }],
        ["prior item without the template", { previous_item: item }],
        ["template for another run", { begin_template: { ...template, run_id: "run-" + "f".repeat(32) } }],
        ["template without operation", { begin_template: noOperation }],
        ["template for another operation", { begin_template: { ...template, operation: "progress" } }],
        ["template with 17 keys", { begin_template: { ...widest, one_more: "<17>" } }],
        ["template value of 257 UTF-8 bytes", { begin_template: { ...template, title: "界".repeat(85) + "ab" } }],
        ["template with a non-string value", { begin_template: { ...template, phase: 1 } }],
        ["template sent as a string", { begin_template: JSON.stringify(template) }],
        ["template as an array", { begin_template: [template] }],
        ["null template", { begin_template: null }]
    ]) {
        check(name + " fails visible", !folds(changes));
    }
    for (const spelling of ["itemId", "item-id", "item_id2", "", "z".repeat(65), "title\n"]) {
        check("template key spelled " + JSON.stringify(spelling) + " fails visible",
            !folds({ begin_template: { ...template, [spelling]: "<x>" } }));
    }
}
{
    const source = "保留 <script>alert(1)</script> 與原句\n\n" + workflowWire + "\n" + workflowImage;
    const parsed = parseBoardWorkflowRecord(source, "user");
    check("exact managed envelope is recognized",
        parsed && parsed.metadata.run_id === workflowMetadata.run_id);
    check("all text before and after the envelope survives byte-for-byte",
        parsed.text === "保留 <script>alert(1)</script> 與原句\n" + workflowImage);
    check("raw metadata remains available for technical disclosure",
        parsed.raw === JSON.stringify(workflowMetadata));
    const html = boardWorkflowRecordHTML(parsed, {
        label: "看板紀錄",
        escape: value => String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;")
            .replaceAll(">", "&gt;").replaceAll('"', "&quot;")
    });
    check("record is collapsed by default and names its non-user authority",
        html.includes("<details") && !html.includes("<details open")
            && html.includes("看板紀錄") && html.includes("managed_ingress"));
    check("raw JSON is escaped rather than executable",
        html.includes("&lt;conversation-id&gt;") && !html.includes("<conversation-id>"));
}
for (const [name, source] of [
    ["assistant turn", workflowWire],
    ["quoted marker", "> " + workflowWire],
    ["backtick fence", "```text\n" + workflowWire + "\n```"],
    ["tilde fence", "~~~\n" + workflowWire + "\n~~~"],
    ["malformed JSON", workflowWire.replace(JSON.stringify(workflowMetadata), "{not-json}")],
    ["wrong authority", workflowWire.replace("metadata-not-user", "user")],
    ["unknown metadata field", workflowWire.replace(/}\n<\/clawdline-workflow>/,
        ',"extra":"field"}\n</clawdline-workflow>')],
    ["indented prose marker", "quote: " + workflowWire + " end quote"]
]) {
    const role = name === "assistant turn" ? "assistant" : "user";
    check(name + " stays untouched", parseBoardWorkflowRecord(source, role) === null);
}
check("multiple envelope candidates fail visible instead of partly hiding prose",
    parseBoardWorkflowRecord(workflowWire + "\n" + workflowWire, "user") === null);
{
    const parsed = parseBoardWorkflowRecord("前段文字\n\n" + workflowWire + "\n後段文字", "user");
    check("ordinary user text after exact metadata remains visible",
        parsed?.text === "前段文字\n後段文字");
}
{
    const conversation='11111111-1111-4111-8111-111111111111';
    const projectId='project-0123456789abcdef01234567';
    const selected={...item('locator-owner','execution',projectId),owner:conversation,
        links:[{kind:'session',targetId:conversation,label:'Old recorded label'}]};
    const p=page({read:async()=>envelope({items:[selected],item:selected,
        projects:[{id:projectId,name:'Project',displayPath:'/project',machine:'mac-b'}]}),
        sessions:()=>[{id:'%8',machine:'mac-b',sessionId:conversation,title:'Readable current title'}]});
    await p.view.open(projectId,selected.id,{id:projectId,machine:'mac-b'});
    const title=p.elements['board-detail'].all('.board-session-label')[0];
    check('Session title itself is a stable clickable link, not a bare terminal id',
        title?.tagName==='A'&&title.textContent==='Readable current title'
        &&title.href.startsWith('https://app.clawdline.com/#session_ref=1&')
        &&title.href.includes('machine=mac-b')&&title.href.includes('conversation='+conversation)
        &&!title.href.includes('%258'));
    p.view.leave();
}
{
    const selected={...item('assignable','execution','a'),sessionAssignment:null};let requested;
    const p=page({read:async()=>envelope({items:[selected],item:selected}),
        assignSession:target=>{requested=target;}});
    await p.view.open('a',selected.id,{id:'a',machine:'mac-b'});
    const action=p.elements['board-detail'].all(n=>n.dataset.boardAction==='assign-session')[0];
    check('detail exposes a deliberate Session assignment entry',!!action);
    action.click();check('assignment entry pins exact item Project and Mac',
        requested?.itemId===selected.id&&requested.projectId==='a'&&requested.machine==='mac-b');
    p.view.leave();
}
{
    const { idleClient } = await import("../Resources/web/app/js/net/cloud-boot.js");
    let transport = idleClient();
    const calls = [];
    const p = page({read: (...args) => { calls.push(args); return transport.board(...args); }});
    // The binder captures its scheduler on construction; use the actual registered callback
    // below to prove automatic recovery rather than substituting a user refresh.
    await p.view.open("a", null, { id: "a", machine: "mac-cold" });
    check("cold Board uses a waiting state rather than a technical error", p.view.state.readStatus === "loading"
        && p.elements["board-status"].textContent.includes("Cloud")
        && !p.elements["board-status"].textContent.includes("is not a function"));
    check("cold Board retains exact route", p.view.state.projectId === "a" && p.view.state.machine === "mac-cold");
    transport = { board: async () => envelope() };
    check("cold Board has one bounded two-second retry", p.timers.size === 1 && p.timerDelays.at(-1) === 2000);
    const retry = [...p.timers.values()][0]; p.timers.clear(); retry(); await flush();
    check("ready transport recovers the same route", p.view.state.selectionLoaded
        && calls.at(-1)[0] === "a" && calls.at(-1)[2] === "mac-cold");
    const retained = p.view.state.items;
    transport = idleClient();
    await p.view.refresh();
    check("reconnecting retains observed data and selection", p.view.state.items === retained
        && p.view.state.selectionLoaded && p.view.state.projectId === "a");
    transport = { board: async () => { throw Object.assign(new Error("denied"), {code:"forbidden"}); } };
    await p.view.refresh();
    check("real refusal is not disguised as connecting", p.view.state.readStatus === "error"
        && p.elements["board-status"].textContent.includes("(forbidden)")
        && !p.elements["board-status"].textContent.includes("denied"));
    p.view.leave();
    check("leaving cancels pending retries", p.timers.size === 0);
}
console.log(`${checks} web board behavioral checks passed`);
