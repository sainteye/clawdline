// Conversations record facts. This reading surface never issues lifecycle commands.
const STATUS = {
    planning: ["Planning", "規劃中", "Clarifying the objective and approach.", "正在釐清目標與執行步驟。"],
    queued: ["Ready to start", "等待開始", "Scheduled; waiting to start.", "已安排，等待開始執行。"],
    execution: ["In progress", "執行中", "Work is underway.", "正在實作與產出。"],
    review_testing: [
        "Review & testing",
        "審查與測試",
        "Checking the result before delivery.",
        "正在檢查成果是否符合目標。"
    ],
    correction: [
        "Making corrections",
        "修正中",
        "Addressing review or test findings.",
        "正在處理審查或測試發現的問題。"
    ],
    verified: [
        "Verified",
        "已驗證",
        "Verification is recorded; integration is next.",
        "已有驗證紀錄，接下來確認整合與落地。"
    ],
    landed: [
        "Landed",
        "已落地",
        "Recorded evidence supports this delivery.",
        "已有成果落地的紀錄，可展開查看依據。"
    ],
    delivered: [
        "Delivered · awaiting confirmation",
        "已交付・待確認",
        "An attempt finished; landing is not established.",
        "執行已交付，尚未確認整個項目已落地。"
    ],
    blocked: [
        "Needs attention",
        "遇到阻礙",
        "An unresolved issue is holding up progress.",
        "有尚未解決的問題，需要協調或修正。"
    ],
    canceled: ["Canceled", "已取消", "Stopped; not counted as completed.", "已停止，不計入完成的成果。"],
    unknown: [
        "Status not established",
        "狀態待釐清",
        "Retained records cannot establish the outcome.",
        "目前保留的紀錄不足以判斷結果。"
    ]
};
const TYPES = {
    feature: ["Feature", "功能"],
    refactor: ["Refactor", "重構"],
    task: ["Task", "任務"],
    bug: ["Bug fix", "修復"],
    coordination: ["Coordination", "協調"],
    epic: ["Epic", "大型計畫"]
};
const PHASES = {
    planning: ["Planning", "規劃"],
    output: ["Main output", "執行產出"],
    review_testing: ["Review & testing", "審查與測試"],
    correction: ["Correction", "修正"],
    integration: ["Integration", "整合"]
};
function clear(node) {
    while (node && node.firstChild) node.removeChild(node.firstChild);
}
function localized(ctx, pair) {
    return pair[/^zh/i.test(ctx.doc.documentElement.lang || "") ? 1 : 0];
}
function words(ctx, en, zh) {
    return localized(ctx, [en, zh]);
}
function el(ctx, parent, tag, value, cls) {
    const node = ctx.doc.createElement(tag);
    if (cls) node.className = cls;
    if (value != null) node.textContent = String(value);
    if (parent) parent.appendChild(node);
    return node;
}
function button(ctx, parent, value, action, run, cls = "board-button") {
    const node = el(ctx, parent, "button", value, cls);
    node.type = "button";
    node.dataset.boardAction = action;
    node.addEventListener("click", run);
    return node;
}
function date(value) {
    if (!value) return "—";
    const at = new Date(typeof value === "number" ? value * 1000 : value);
    return Number.isNaN(at.getTime())
        ? "—"
        : at.toLocaleString(undefined, {
              month: "short",
              day: "numeric",
              hour: "2-digit",
              minute: "2-digit"
          });
}
function number(value) {
    return new Intl.NumberFormat().format(value);
}
function safeURL(value) {
    try {
        const url = new URL(value);
        return ["http:", "https:"].includes(url.protocol) ? url.href : null;
    } catch {
        return null;
    }
}
export function resolveBoardSession(sessions, conversationID) {
    const matches = (sessions || []).filter((row) => row.sessionId === conversationID);
    return matches.length === 1
        ? { id: matches[0].id }
        : {
              error: matches.length ? "session_ambiguous" : "session_unavailable"
          };
}
export function boardProgress(item) {
    if (item.type === "coordination") return "not_applicable";
    // Compatibility uses only explicit lifecycle state, never task success, title or age.
    const state =
        (item.progress && item.progress.state) ||
        {
            backlog: "planning",
            ready: "queued",
            integrated: "landed",
            closed: "landed"
        }[item.state] ||
        item.state;
    return STATUS[state] ? state : "unknown";
}
function say(ctx, item) {
    if (item.type === "coordination")
        return words(
                ctx,
                "Coordination is recorded by time period or handoff, not completion status.",
                "依時段或交接記錄協調，不套用完成狀態。"
        );
    if (boardProgress(item) === "landed" && ((item.progress && item.progress.warningCodes) || []).length)
        return words(
            ctx,
            "Landed. Some earlier workflow records are incomplete.",
            "已確認落地；部分較早的流程紀錄尚未齊全。"
        );
    return localized(ctx, STATUS[boardProgress(item)].slice(2));
}
function badge(ctx, parent, item) {
    if (item.type === "coordination")
        return el(ctx, parent, "span", words(ctx, "Coordination record", "協調紀錄"), "board-type");
    if (item.type === "task" && boardProgress(item) === "landed")
        return el(ctx, parent, "span", words(ctx, "Output completed", "成果已完成"), "board-state-pill board-state-landed");
    return el(
        ctx,
        parent,
        "span",
        localized(ctx, STATUS[boardProgress(item)]),
        "board-state-pill board-state-" + boardProgress(item)
    );
}
function usageText(ctx, item) {
    const u = item.usage;
    if (!u || ["absent", "unavailable"].includes(u.state)) return words(ctx, "Usage unknown", "用量未知");
    if (u.state === "qualified" || (u.coverageReasons || []).length)
        return (
            words(ctx, "Measured ", "已量測 ") +
            (typeof u.measured === "number" ? number(u.measured) : "—") +
            words(ctx, " tokens · qualified", " tokens・來源或歸屬待確認")
        );
    if (u.state === "present" && typeof u.total === "number") return number(u.total) + " tokens";
    if (typeof u.measured === "number")
        return "≥ " + number(u.measured) + words(ctx, " tokens · partial", " tokens・部分紀錄");
    return words(ctx, "Usage unknown", "用量未知");
}
function journey(ctx, parent, item) {
    if (["coordination", "task"].includes(item.type)) return;
    const state = boardProgress(item),
        step = {
            planning: 0,
            queued: 0,
            execution: 1,
            correction: 1,
            review_testing: 2,
            verified: 2,
            landed: 3
        }[state];
    const track = el(ctx, parent, "ol", null, "board-journey");
    track.setAttribute("aria-label", words(ctx, "Recorded progress", "紀錄中的進度"));
    [
        ["Plan", "規劃"],
        ["Execute", "執行"],
        ["Verify", "驗證"],
        ["Land", "落地"]
    ].forEach((pair, index) => {
        // Landing does not imply all earlier proof survived. Only the observed stage is lit.
        const current = index === step;
        const row = el(ctx, track, "li", null, "board-step" + (current ? " is-current" : ""));
        if (current) row.setAttribute("aria-current", "step");
        el(ctx, row, "span", current ? (state === "landed" ? "✓" : "●") : "·", "board-step-dot");
        el(ctx, row, "span", localized(ctx, pair));
    });
}
function card(ctx, item) {
    const node = button(ctx, null, null, "item", () => ctx.openItem(item.id), "board-item-card");
    node.dataset.boardItemId = item.id;
    node.dataset.boardType = item.type;
    node.dataset.boardState = boardProgress(item);
    const top = el(ctx, node, "span", null, "board-card-top");
    badge(ctx, top, item);
    el(ctx, top, "span", localized(ctx, TYPES[item.type] || [item.type, item.type]), "board-type");
    el(ctx, node, "strong", item.title || item.key || item.id, "board-card-title");
    if (item.summary) el(ctx, node, "span", item.summary, "board-card-summary");
    const blocker = (item.obligations || []).find((row) => row.blocking && !row.resolved);
    el(ctx, node, "span", blocker ? blocker.title : say(ctx, item), "board-card-next");
    journey(ctx, node, item);
    const foot = el(ctx, node, "span", null, "board-card-facts"),
        checks = (item.checklist || []).filter((row) => row.required);
    if (checks.length && item.type !== "coordination")
        el(
            ctx,
            foot,
            "span",
            checks.filter((row) => ["passed", "not_applicable"].includes(row.status)).length +
                "/" +
                checks.length +
                words(ctx, " checks", " 項檢查")
        );
    el(ctx, foot, "span", usageText(ctx, item));
    el(ctx, foot, "span", date(item.updatedAt));
    return node;
}
function section(ctx, parent, title, collapsed = false, key = title) {
    const node = el(ctx, parent, collapsed ? "details" : "section", null, "board-section");
    node.dataset.boardSection = key;
    if (collapsed) node.open = !!(ctx.openSections && ctx.openSections.has(key));
    el(ctx, node, collapsed ? "summary" : "h2", title, "board-section-title");
    return node;
}
function fact(ctx, parent, label, value) {
    const row = el(ctx, parent, "div", null, "board-fact");
    el(ctx, row, "span", label, "board-fact-label");
    el(ctx, row, "span", value == null || value === "" ? "—" : value, "board-fact-value");
}
function detail(ctx) {
    const item = ctx.state.item,
        target = ctx.e["board-detail"];
    clear(target);
    if (!item) {
        el(
            ctx,
            target,
            "p",
            !ctx.state.selectionLoaded && ctx.state.readStatus !== "error"
                ? words(ctx, "Loading this item's details…", "正在載入這個項目的詳細紀錄…")
                : words(ctx, "This item is unavailable.", "目前無法讀取這個項目。"),
            "board-empty"
        );
        return;
    }
    const head = el(ctx, target, "header", null, "board-detail-header");
    badge(ctx, head, item);
    el(ctx, head, "h2", item.title, "board-detail-title");
    if (item.summary) el(ctx, head, "p", item.summary, "board-detail-summary");
    journey(ctx, head, item);
    el(ctx, head, "p", say(ctx, item), "board-detail-summary");
    if (["task", "bug"].includes(item.type))
        el(ctx, head, "p", usageText(ctx, item), "board-usage-value");
    if (
        !["landed", "not_applicable"].includes(boardProgress(item)) &&
        item.progress &&
        item.progress.evidenceCounts &&
        item.progress.evidenceCounts.landings > 0
    )
        el(
            ctx,
            head,
            "p",
            words(
                ctx,
                "Earlier landing records are retained below; the progress above describes the current work.",
                "下方仍保留先前的落地紀錄；上方進度呈現目前這一輪工作。"
            ),
            "board-section-help"
        );
    const specialized = item.typeDetails || {};
    const topics =
        item.type === "bug"
            ? [
                  ["rootCause", "Why it happened", "為什麼會發生"],
                  ["lessons", "What future work should know", "排錯經驗與盲點"]
              ]
            : item.type === "coordination"
              ? [
                    ["outcomes", "What coordination achieved", "協調了什麼、發揮什麼功效"],
                    ["difficulties", "Difficulties encountered", "遇到的困難"],
                    ["improvements", "What to improve next", "之後如何改進"]
                ]
              : [];
    topics.forEach(([key, en, chinese]) => {
        const part = section(ctx, target, words(ctx, en, chinese));
        el(
            ctx,
            part,
            "p",
            specialized[key] ||
                words(ctx, "Not yet recorded in the conversation record.", "對話紀錄尚未補上這段說明。"),
            "board-detail-summary"
        );
    });
    if (item.type === "coordination") {
        const part = section(ctx, target, words(ctx, "Coordination period & handoff", "協調時段與交接"));
        if (!(item.spans || []).length)
            el(
                ctx,
                part,
                "p",
                words(
                    ctx,
                    "Coordination time boundaries have not been recorded.",
                    "尚未記錄協調時段的起訖。"
                ),
                "board-section-help"
            );
        (item.spans || []).forEach((row) =>
            fact(
                ctx,
                part,
                row.sessionId,
                date(row.startedAt) +
                    " – " +
                    (row.endedAt ? date(row.endedAt) : words(ctx, "Ongoing", "持續中"))
            )
        );
        (item.links || [])
            .filter((row) => row.kind === "coordinates")
            .forEach((row) =>
                fact(ctx, part, words(ctx, "Related work", "協調項目"), row.label || row.targetId)
            );
        if (item.handoff)
            fact(ctx, part, words(ctx, "Handoff", "交接"), item.handoff.note || item.handoff.proposedOwner);
    }
    const blockers = (item.obligations || []).filter((row) => !row.resolved);
    if (blockers.length) {
        const part = section(ctx, target, words(ctx, "What is holding this up", "目前需要處理"));
        blockers.forEach((row) => fact(ctx, part, row.title, row.owner));
    }
    if (item.type !== "coordination" && (item.checklist || []).length) {
        const part = section(ctx, target, words(ctx, "Progress toward the objective", "目標完成進度"));
        const names = {
            todo: ["Not started", "尚未開始"],
            doing: ["In progress", "進行中"],
            passed: ["Passed", "通過"],
            failed: ["Needs correction", "待修正"],
            not_applicable: ["Not applicable", "不適用"]
        };
        item.checklist.forEach((row) => {
            const line = el(ctx, part, "div", null, "board-check-row board-check-" + row.status);
            el(
                ctx,
                line,
                "span",
                row.status === "passed" ? "✓" : row.status === "failed" ? "!" : "○",
                "board-check-icon"
            );
            el(ctx, line, "span", row.title, "board-check-title");
            el(
                ctx,
                line,
                "span",
                localized(ctx, names[row.status] || ["Unknown", "未知"]),
                "board-check-status"
            );
        });
    }
    if (["feature", "refactor", "epic"].includes(item.type) && (item.milestones || []).length) {
        const part = section(ctx, target, words(ctx, "Milestones", "里程碑"));
        item.milestones.forEach((row) =>
            fact(ctx, part, row.title, ["passed", "not_applicable"].includes(row.status) ? "✓" : "○")
        );
    }
    if ((item.artifacts || []).length) {
        const part = section(ctx, target, words(ctx, "What this produced", "產出了什麼"));
        item.artifacts.forEach((row) => {
            const url = safeURL(row.url),
                link = el(ctx, part, url ? "a" : "span", row.title || row.url, "board-artifact-link");
            if (url) {
                link.href = url;
                link.target = "_blank";
                link.rel = "noopener noreferrer";
            }
        });
    }
    const usage = section(ctx, target, words(ctx, "Token spending", "Token 花費"), true);
    el(ctx, usage, "p", usageText(ctx, item), "board-usage-value");
    Object.entries((item.usage && item.usage.phases) || {}).forEach(([key, amount]) => {
        const value = typeof amount === "number" ? amount : amount.total;
        fact(
            ctx,
            usage,
            localized(ctx, PHASES[key] || [key, key]),
            typeof value === "number" ? number(value) + " tokens" : words(ctx, "Unknown", "未知")
        );
    });
    if (
        !item.usage ||
        item.usage.undeclaredRows ||
        item.usage.phaseReason === "historical_phase_not_measured"
    )
        el(
            ctx,
            usage,
            "p",
            words(
                ctx,
                "Undeclared phases are not estimated from elapsed time.",
                "未宣告階段的花費不會依時間比例推估。"
            ),
            "board-section-help"
        );
    ((item.usage && item.usage.costSeries) || []).forEach((row) =>
        fact(ctx, usage, words(ctx, "Cost", "費用"), number(row.value) + " " + row.unit + " · " + row.basis)
    );
    const records = section(ctx, target, words(ctx, "Sessions & records", "相關對話與紀錄"), true);
    (item.links || []).forEach((row) => {
        const line = el(ctx, records, "div", null, "board-link-row");
        line.dataset.boardLinkKind = row.kind;
        if (row.kind === "session")
            button(ctx, line, row.label || row.targetId, "open-session", () => {
                Promise.resolve()
                    .then(() => ctx.env.openSession && ctx.env.openSession(row.targetId))
                    .then((answer) => {
                        if (answer && answer.error)
                            ctx.status(
                                words(
                                    ctx,
                                    "This record has no unique live Session to open.",
                                    "這段紀錄目前沒有可唯一對應的執行中對話。"
                                ),
                                true
                            );
                    })
                    .catch((error) => ctx.status(error.message, true));
            });
        else
            fact(
                ctx,
                line,
                row.kind === "worktree"
                    ? "Worktree"
                    : row.kind === "task"
                      ? words(ctx, "Attempt", "執行紀錄")
                      : row.kind,
                row.label || row.targetId
            );
    });
    (item.spans || []).forEach((row) =>
        fact(
            ctx,
            records,
            localized(ctx, PHASES[row.phase] || [row.phase, row.phase]),
            date(row.startedAt) + " – " + (row.endedAt ? date(row.endedAt) : words(ctx, "Ongoing", "持續中"))
        )
    );
    const proof = section(ctx, target, words(ctx, "Why this status?", "狀態的依據"), true),
        current = item.currentEvidence || {};
    if (item.progress && item.progress.reason)
        fact(ctx, proof, words(ctx, "Basis", "判讀依據"), item.progress.reason);
    [
        ["verifications", "verificationId", ["Verification", "驗證"]],
        ["landings", "landingId", ["Landing", "落地"]],
        ["artifactAcceptances", "artifactAcceptanceId", ["Accepted output", "成果驗收"]],
        ["findings", null, ["Finding", "問題紀錄"]],
        ["evidenceSummaries", null, ["Attempt summary", "執行摘要"]]
    ].forEach(([key, pointer, label]) => {
        (item[key] || []).forEach((row) => {
            const part = section(ctx, proof, localized(ctx, label));
            el(ctx, part, "p", row.summary || row.id);
            fact(
                ctx,
                part,
                words(ctx, "Validity", "適用性"),
                pointer && row.id === current[pointer]
                    ? words(ctx, "Current", "目前有效")
                    : words(ctx, "Historical record · not current acceptance", "歷史紀錄・不代表目前驗收")
            );
            fact(ctx, part, words(ctx, "Subject", "驗證對象"), row.subject);
            fact(ctx, part, words(ctx, "Receipt", "收據"), row.sourceId);
        });
    });
    const history = section(ctx, target, words(ctx, "Timeline", "發生過的事"), true);
    (item.history || [])
        .slice()
        .reverse()
        .forEach((row) => fact(ctx, history, date(row.at), row.summary || row.kind));
    Object.entries(item.projection || {}).forEach(([key, value]) => {
        if (value.omittedCount > 0)
            el(
                ctx,
                target,
                "p",
                value.omittedCount + words(ctx, " records omitted from this view: ", " 筆紀錄未載入：") + key,
                "board-section-help"
            );
    });
    const evicted = Math.max(
        0,
        (item.historyDroppedCount || 0) -
            ((item.projection && item.projection.history && item.projection.history.omittedCount) || 0)
    );
    if (evicted)
        el(
            ctx,
            target,
            "p",
            evicted + words(ctx, " older records are no longer retained.", " 筆較早紀錄已超過保留上限。"),
            "board-section-help"
        );
}
export function enterProjectBoard(page, navigate) {
    if (page.state.projectId) return page.enter();
    // Defer until the page router has completed its current navigation bookkeeping.
    return Promise.resolve().then(() => navigate("projects"));
}

export function bindBoardPage(elements, environment = {}) {
    const ctx = {
        e: elements,
        env: environment,
        doc: environment.document || globalThis.document
    };
    const state = (ctx.state = {
        view: "projects",
        projects: [],
        items: [],
        item: null,
        projectId: null,
        projectPresentation: null,
        selectionLoaded: false,
        readStatus: "loading",
        itemId: null,
        revision: -1,
        enabled: true,
        active: false,
        readTicket: 0
    });
    let timer = null,
        inflight = null,
        opened = null;
    const schedule = environment.setTimeout || globalThis.setTimeout,
        unschedule = environment.clearTimeout || globalThis.clearTimeout;
    function stopTimer() {
        if (timer != null) unschedule(timer);
        timer = null;
    }
    ctx.status = (message, error = false) => {
        clear(elements["board-status"]);
        elements["board-status"].className = "board-status" + (error ? " board-status-error" : "");
        el(ctx, elements["board-status"], "span", message);
        if (error) button(ctx, elements["board-status"], words(ctx, "Retry", "重試"), "retry", refresh);
    };
    function renderBody() {
        if (elements.board) elements.board.dataset.boardView = state.view;
        const project = state.projects.find((row) => row.id === state.projectId)
            || state.projectPresentation;
        if (elements["board-title"])
            elements["board-title"].textContent = project
                ? project.label || project.name
                : words(ctx, "Projects", "專案");
        const mark = elements["board-project-mark"];
        if (mark) mark.hidden = !(project && environment.drawIcon
            && environment.drawIcon(mark, project.icon, 5));
        if (elements["board-title"] && elements["board-title"].style)
            elements["board-title"].style.color = project && project.icon && environment.tint
                ? environment.tint(project.icon.accent) : "";
        if (elements["board-subtitle"])
            elements["board-subtitle"].textContent = project
                ? project.displayPath || project.path || words(
                      ctx,
                      "Work takes shape in conversation. Progress follows the evidence.",
                      "從對話開始，依實際執行與成果自動更新。"
                  )
                : words(ctx, "Choose a project to understand its work.", "選擇專案，了解正在發生的事。");
        elements["board-back"].textContent = state.itemId
            ? words(ctx, "← All work", "← 專案項目")
            : words(ctx, "← Projects", "← 專案");
        elements["board-refresh"].textContent = words(ctx, "Refresh", "重新整理");
        elements["board-search"].placeholder = words(
            ctx,
            "Find work in this project…",
            "搜尋這個專案的項目…"
        );
        elements["board-search"].hidden = !state.projectId || !!state.itemId;
        elements["board-items"].hidden = !!state.itemId;
        elements["board-detail"].hidden = !state.itemId;
        if (state.itemId) {
            detail(ctx);
            return;
        }
        const target = elements["board-items"];
        clear(target);
        target.className = "board-items";
        target.setAttribute("aria-busy", String(!state.selectionLoaded));
        if (!state.selectionLoaded) {
            el(ctx, target, "p", state.readStatus === "error"
                ? words(ctx, "Project records could not be loaded. Retry to continue.", "專案紀錄尚未載入，請重試。")
                : words(ctx, "Loading this project's work…", "正在載入這個專案的工作項目…"), "board-loading");
            return;
        }
        if (!state.projectId) {
            state.projects.forEach((row) => {
                const node = button(ctx, target, null, "project", () => open(row.id), "board-project-card");
                el(
                    ctx,
                    node,
                    "span",
                    (row.label || row.name || "P").slice(0, 1).toUpperCase(),
                    "board-project-mark"
                );
                const body = el(ctx, node, "span", null, "board-project-copy");
                el(ctx, body, "strong", row.label || row.name);
                el(ctx, body, "span", row.itemCount + words(ctx, " work items", " 個工作項目"));
                el(ctx, node, "span", "↗", "board-project-arrow");
            });
            if (!state.projects.length)
                el(
                    ctx,
                    target,
                    "p",
                    words(
                        ctx,
                        "Projects appear after you start a conversation in a project.",
                        "在專案中開始對話後，專案會出現在這裡。"
                    ),
                    "board-empty"
                );
            return;
        }
        const all = state.items.filter((row) => row.projectId === state.projectId),
            query = elements["board-search"].value.trim().toLocaleLowerCase();
        const rows = all.filter(
            (row) =>
                !query ||
                [row.title, row.summary, row.key, row.owner].join(" ").toLocaleLowerCase().includes(query)
        );
        const delivery = all.filter((row) => row.type !== "coordination"),
            coordinated = rows.filter((row) => row.type === "coordination");
        const active = rows.filter(
                (row) => row.type !== "coordination" && !["landed", "canceled"].includes(boardProgress(row))
            ),
            history = rows.filter((row) => ["landed", "canceled"].includes(boardProgress(row)));
        const overview = el(ctx, target, "div", null, "board-overview");
        const summary = project && project.summary;
        const modelCount = (key, loaded) => summary && Number.isInteger(summary[key]) && summary[key] >= 0
            ? summary[key] : loaded;
        [
            [
                modelCount("open", delivery.filter((row) => !["landed", "canceled"].includes(boardProgress(row))).length),
                "Open work",
                "尚在推進"
            ],
            [
                modelCount("needsClarity", delivery.filter((row) => ["blocked", "delivered", "unknown"].includes(boardProgress(row)))
                    .length),
                "Needs clarity",
                "需要釐清"
            ],
            [modelCount("landed", delivery.filter((row) => boardProgress(row) === "landed").length), "Landed", "已落地"]
        ].forEach(([count, en, chinese]) => {
            const stat = el(ctx, overview, "div", null, "board-overview-stat");
            el(ctx, stat, "strong", count);
            el(ctx, stat, "span", words(ctx, en, chinese));
        });
        if (active.length) {
            const part = section(ctx, target, words(ctx, "In focus", "現在的工作")),
                list = el(ctx, part, "div", null, "board-card-grid");
            active
                .sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0))
                .forEach((row) => list.appendChild(card(ctx, row)));
        } else
            el(
                ctx,
                target,
                "p",
                query
                    ? words(ctx, "No matching open work.", "沒有符合搜尋的進行中項目。")
                    : modelCount("open", 0) > 0 ? words(ctx,
                          "More work is recorded than this view has loaded.",
                          "專案還有進行中的工作，此畫面尚未載入。") : words(
                          ctx,
                          "No open work. Tell your assistant what you would like to do next.",
                          "目前沒有待推進的項目。想做什麼，直接在對話中告訴 assistant。"
                      ),
                "board-empty"
            );
        if (history.length) {
            const part = section(
                ctx,
                target,
                words(ctx, "Past work", "歷史成果") + " · " + history.length,
                true,
                "delivery-history"
            );
            part.className += " board-history-group";
            const list = el(ctx, part, "div", null, "board-card-grid");
            history
                .sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0))
                .forEach((row) => list.appendChild(card(ctx, row)));
        }
        if (coordinated.length) {
            const part = section(
                ctx,
                target,
                words(ctx, "Coordination records", "協調紀錄") + " · " + coordinated.length,
                true,
                "coordination-records"
            );
            part.className += " board-coordination-group";
            el(
                ctx,
                part,
                "p",
                words(
                    ctx,
                    "Time periods, related work and handoffs — not a completion queue.",
                    "以時段、關聯工作與交接整理，不計入項目完成率。"
                ),
                "board-section-help"
            );
            const list = el(ctx, part, "div", null, "board-card-grid");
            coordinated.forEach((row) => list.appendChild(card(ctx, row)));
        }
    }
    function render() {
        const nodes = [];
        function visit(node) {
            if (!node) return;
            nodes.push(node);
            Array.from(node.children || []).forEach(visit);
        }
        visit(elements["board-items"]);
        visit(elements["board-detail"]);
        ctx.openSections = new Set(
            nodes
                .filter((node) => node.tagName === "DETAILS" && node.open)
                .map((node) => node.dataset.boardSection)
        );
        const top = elements.board && elements.board.scrollTop;
        const focus = ctx.doc.activeElement;
        const focusKey =
            focus &&
            (focus.dataset.boardItemId ||
                (focus.tagName === "SUMMARY" && focus.parentNode.dataset.boardSection));
        renderBody();
        if (typeof top === "number") elements.board.scrollTop = top;
        if (focusKey) {
            nodes.length = 0;
            visit(elements["board-items"]);
            visit(elements["board-detail"]);
            const replacement = nodes.find(
                (node) =>
                    node.dataset.boardItemId === focusKey ||
                    (node.tagName === "SUMMARY" && node.parentNode.dataset.boardSection === focusKey)
            );
            if (replacement && replacement.focus) replacement.focus({ preventScroll: true });
        }
    }
    function later() {
        stopTimer();
        if (state.active && state.enabled) {
            timer = schedule(() => {
                timer = null;
                if (ctx.doc.hidden) later();
                else load(true);
            }, state.readStatus === "loading" ? 2000 : 15000);
            if (timer && timer.unref) timer.unref();
        }
    }
    function load(quiet = false) {
        stopTimer();
        const ticket = ++state.readTicket,
            project = state.projectId,
            item = state.itemId;
        if (!quiet) ctx.status(words(ctx, "Reading project records…", "正在讀取專案紀錄…"));
        const promise = Promise.resolve()
            .then(() => {
                if (!environment.read) throw new Error(words(ctx, "Board unavailable", "無法讀取看板"));
                return environment.read(project || undefined, item || undefined);
            })
            .then((answer) => {
                if (!state.active || ticket !== state.readTicket) return;
                const board = answer && answer.board;
                if (
                    !board ||
                    board.schemaVersion !== 1 ||
                    !Array.isArray(board.projects) ||
                    !Array.isArray(board.items)
                )
                    throw new Error(words(ctx, "Board response is incomplete.", "看板回應不完整。"));
                if (typeof board.revision !== "number" || board.revision < state.revision) return;
                state.revision = board.revision;
                state.projects = board.projects;
                state.readStatus = board.readState && board.readState.status || "ready";
                if (project && !["loading", "error"].includes(state.readStatus)
                    && !board.projects.some((row) => row.id === project)) {
                    const unavailable = new Error(words(ctx,
                        "Project records are unavailable; the directory response does not contain this project.",
                        "專案紀錄目前無法取得；目錄回應未包含這個專案。"));
                    unavailable.code = "project_not_found";
                    throw unavailable;
                }
                if (state.readStatus !== "loading" && state.readStatus !== "error") {
                    state.selectionLoaded = true;
                    state.items = board.items;
                    state.item =
                        board.item && board.item.id === item && board.item.projectId === project
                            ? board.item
                            : null;
                }
                state.enabled = board.enabled !== false;
                if (environment.onMode) environment.onMode(board);
                if (environment.onProjects) environment.onProjects(board.projects);
                render();
                let message = !state.enabled
                    ? words(
                          ctx,
                          "Standard mode · retained history is read-only.",
                          "一般模式・只顯示已保留的歷史紀錄。"
                      )
                    : words(ctx, "Automatically updated from recorded activity", "依執行紀錄自動更新");
                if (state.readStatus === "loading") message = words(ctx,
                    "Preparing project records in the background…", "正在背景整理專案紀錄…");
                if (state.readStatus === "stale") message = words(ctx,
                    "Showing the last available records; updating in the background.", "目前顯示上次可用的紀錄，正在背景更新。");
                if (state.readStatus === "error") message = words(ctx,
                    "Project records are temporarily unavailable; retrying in the background.", "專案紀錄暫時無法取得，稍後會在背景重試。");
                if (board.source && board.source.observedAt) message += " · " + date(board.source.observedAt);
                if (board.truncated)
                    message += words(ctx, " · Some records are not loaded", "・部分紀錄未載入");
                const ingestion = board.source && board.source.ingestion;
                if (ingestion && ingestion.issueCount)
                    message +=
                        words(ctx, " · Source synchronization incomplete: ", "・來源同步不完整：") +
                        (ingestion.reasons || []).join(", ");
                ctx.status(message);
            })
            .catch((error) => {
                if (state.active && ticket === state.readTicket) {
                    state.readStatus = "error";
                    render();
                    ctx.status(
                        words(
                            ctx,
                            "Could not update. Displayed records may be old. ",
                            "更新失敗，畫面上的紀錄可能不是最新。"
                        ) + (error.message || ""),
                        true
                    );
                }
            })
            .finally(() => {
                if (inflight === promise) inflight = null;
                if (state.active && ticket === state.readTicket) later();
            });
        inflight = promise;
        return promise;
    }
    function refresh() {
        return inflight || load();
    }
    function open(project, item, presentation) {
        state.active = true;
        if (state.projectId !== project) state.projectPresentation = null;
        if (state.projectId !== (project || null) || state.itemId !== (item || null)) state.revision = -1;
        if (presentation) state.projectPresentation = presentation;
        else if (!state.projectPresentation)
            state.projectPresentation = state.projects.find((row) => row.id === project) || null;
        state.selectionLoaded = false;
        state.readStatus = "loading";
        state.projectId = project || null;
        state.itemId = item || null;
        state.item = null;
        state.view = item ? "detail" : project ? "items" : "projects";
        elements["board-search"].value = "";
        render();
        if (elements.board) elements.board.scrollTop = 0;
        opened = load();
        const request = opened;
        request.finally(() => {
            if (opened === request) opened = null;
        });
        return request;
    }
    ctx.openItem = (item) => open(state.projectId, item);
    function escape() {
        if (state.itemId) return open(state.projectId);
        if (state.projectId) {
            if (environment.navigate) environment.navigate("projects");
            else return open();
        } else if (environment.navigate) environment.navigate("sessions");
    }
    elements["board-search"].addEventListener("input", render);
    elements["board-back"].addEventListener("click", escape);
    elements["board-refresh"].addEventListener("click", refresh);
    function leave() {
        state.active = false;
        ++state.readTicket;
        stopTimer();
        inflight = null;
        opened = null;
    }
    function enter(project, item) {
        if (opened) return opened;
        return arguments.length ? open(project, item) : open(state.projectId, state.itemId);
    }
    return { enter, leave, refresh, escape, open, state };
}
