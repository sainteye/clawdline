import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { bindBoardPage, boardProgress, resolveBoardSession } from "../Resources/web/app/js/view/board.js";
import * as boardModule from "../Resources/web/app/js/view/board.js";

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
    const p = page({ read: () => Promise.resolve(envelope({
        projects: [{ id: "a", name: "Clawdline", summary: { open: 24, needsClarity: 6, landed: 90 } }],
        items: [], truncated: true
    })) });
    await p.view.open("a");
    check("Project totals come from its materialized summary, not loaded item rows",
        p.elements["board-items"].all(".board-overview-stat").map(row => row.textContent).join("|")
            === "24尚在推進|6需要釐清|90已落地");
    check("partial item projection does not contradict a nonempty Project model",
        !p.elements["board-items"].textContent.includes("目前沒有待推進的項目"));
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
    check("four scoped items rendered", p.elements["board-items"].all(".board-item-card").length === 4);
    check("payload is literal text", p.elements["board-items"].textContent.includes("<literal>"));
    const history = p.elements["board-items"].all(".board-history-group")[0];
    check("history is a collapsible region", history.tagName === "DETAILS");
    check("landed and canceled retained in history", history.all(".board-item-card").length === 2);
    check("delivery alone stays outside completed history", !history.textContent.includes("delivered"));
    const stats = p.elements["board-items"].all(".board-overview-stat");
    check("canceled is excluded from landed count", stats[2].textContent === "1已落地");
    check("visual progress exists", p.elements["board-items"].all(".board-journey").length === 4);
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
            { title: "Safe result", url: "https://example.test/result" },
            { title: "Unsafe", url: "javascript:alert(1)" }
        ],
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
        projection: { history: { omittedCount: 99 } },
        historyDroppedCount: 101
    };
    const p = page(
        {
            read: () =>
                Promise.resolve(
                    envelope({
                        item: detail,
                        source: {
                            ingestion: {
                                issueCount: 1,
                                reasons: ["link_capacity"]
                            }
                        }
                    })
                ),
            openSession: () => ({ error: "session_unavailable" })
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
    check(
        "safe result has opener protection",
        target.all((n) => n.tagName === "A")[0].rel === "noopener noreferrer"
    );
    check("unsafe scheme is not an anchor", target.all((n) => n.tagName === "A").length === 1);
    check("partial usage is not an exact total", target.textContent.includes("≥ 120"));
    check("source gaps remain visible", p.elements["board-status"].textContent.includes("link_capacity"));
    target.all((n) => n.dataset.boardAction === "open-session")[0].click();
    await flush();
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
        p.elements["board-items"].all(".board-item-card").length === 4
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
