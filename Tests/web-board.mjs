import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { bindBoardPage, boardProgress, resolveBoardSession } from "../Resources/web/app/js/view/board.js";

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
check("explicit server progress wins old backlog", boardProgress(rows[1]) === "landed");
check(
    "unknown server status is not called complete",
    boardProgress({
        state: "closed",
        progress: { state: "new-server-status" }
    }) === "unknown"
);
check("task success is not landing", boardProgress({ state: "backlog", success: true }) === "planning");
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
    const p = page({ read: () => Promise.resolve(envelope({ enabled: false })) }, "en");
    await p.view.enter();
    check(
        "disabled mode retains read-only history",
        p.elements["board-status"].textContent.includes("read-only")
    );
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
        }
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
