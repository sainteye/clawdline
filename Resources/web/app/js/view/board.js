import { boardReportSelection } from "../net/client.js";
import { CANONICAL_DOCUMENT_ORIGIN, documentLocatorFromHash } from "../net/document-links.js";

const UUID_PATTERN = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;

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
    settled: ["Execution finished", "執行已結束", "The root confirmed no code landing is needed for this execution.", "這段工作已結束，負責人確認不需要程式碼落地。"],
    delivered: [
        "Task output received · stage unconfirmed",
        "已有任務交付・階段待確認",
        "A task produced output. This does not establish the whole item's verification or landing.",
        "已有任務產出；尚不能據此判定整個項目已驗證或落地。"
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
// Stable monochrome silhouettes; type color never substitutes for the readable label.
const TYPE_ICONS = {
    feature: "M12 3v18M3 12h18M5.5 5.5l13 13M18.5 5.5l-13 13",
    refactor: "M4 7h13l-3-3m3 3-3 3M20 17H7l3-3m-3 3 3 3",
    task: "M6 3h9l3 3v15H6zM9 12l2 2 4-4M14 3v4h4",
    bug: "M8 8h8v8a4 4 0 0 1-8 0zM9 8V6a3 3 0 0 1 6 0v2M4 10h4m8 0h4M4 15h4m8 0h4M5 21l4-3m6 0 4 3M12 9v10",
    coordination: "M9 5a3 3 0 1 1-6 0 3 3 0 0 1 6 0M21 5a3 3 0 1 1-6 0 3 3 0 0 1 6 0M15 19a3 3 0 1 1-6 0 3 3 0 0 1 6 0M6 10v3l4 3m8-6v3l-4 3M10 5h4",
    epic: "M12 2 2 7l10 5 10-5zM2 12l10 5 10-5M2 17l10 5 10-5"
};
const PHASES = {
    planning: ["Planning", "規劃"],
    output: ["Main output", "執行產出"],
    review_testing: ["Review & testing", "審查與測試"],
    correction: ["Correction", "修正"],
    integration: ["Integration", "整合"]
};
export const CANONICAL_BOARD_ORIGIN = "https://app.clawdline.com";
const PROJECT_ID = /^project-[0-9a-f]{24}$/;
const ITEM_ID = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;

export function boardLocatorFromHash(hash) {
    const source = String(hash || "").replace(/^#/, "");
    // URLSearchParams repairs malformed percent escapes with U+FFFD. Validate the wire first so
    // a corrupt machine identity becomes a route error instead of a request to an invented Mac.
    try {
        source.split("&").forEach(part => part.split("=").forEach(component =>
            decodeURIComponent(component.replace(/\+/g, " "))));
    } catch (_) { return null; }
    const params = new URLSearchParams(source);
    const allowed = new Set(["page", "machine", "project", "item"]);
    for (const key of params.keys()) if (!allowed.has(key)) return null;
    for (const key of allowed) if (params.getAll(key).length > 1) return null;
    const machine = params.get("machine"), project = params.get("project"), item = params.get("item");
    if (params.get("page") !== "board" || !machine || machine === "this-mac"
        || new TextEncoder().encode(machine).length > 200
        || /[\u0000-\u001f\u007f]/.test(machine) || !PROJECT_ID.test(project || "")
        || (item != null && !ITEM_ID.test(item))) return null;
    return { machine, project, item: item || null };
}

export function boardShareURL(machine, project, item) {
    const locator = boardLocatorFromHash("#page=board&machine=" + encodeURIComponent(machine || "")
        + "&project=" + encodeURIComponent(project || "")
        + (item ? "&item=" + encodeURIComponent(item) : ""));
    if (!locator) return null;
    const query = new URLSearchParams({ page: "board", machine: locator.machine,
        project: locator.project });
    if (locator.item) query.set("item", locator.item);
    return CANONICAL_BOARD_ORIGIN + "/#" + query.toString();
}
function clear(node) {
    while (node && node.firstChild) node.removeChild(node.firstChild);
}
function localized(ctx, pair) {
    return pair[/^zh/i.test(ctx.doc.documentElement.lang || "") ? 1 : 0];
}
function words(ctx, en, zh) {
    return localized(ctx, [en, zh]);
}
function readingLocale(value) {
    const locale = String(value || "en").toLowerCase();
    if (/^zh-(tw|hant|hk)/.test(locale)) return "zh-hant";
    if (/^zh/.test(locale)) return "zh-hans";
    return locale.split("-")[0];
}
function reading(ctx, item) {
    const locale = readingLocale(ctx.doc.documentElement.lang);
    const variants = item.presentation && item.presentation.authority === "narrative_only"
        ? item.presentation.variants || [] : [];
    const current = variants.find(row => row.status === "current" && readingLocale(row.locale) === locale);
    return current || { title: item.title, summary: item.summary === "Retained broker execution record." ? "" : item.summary };
}
function shortTitle(value) {
    const text = String(value || "").trim();
    return text.length > 100 ? text.slice(0, 100) + "…" : text;
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
function canonicalDocumentURL(value) {
    try {
        const url = new URL(value);
        if (url.origin !== CANONICAL_DOCUMENT_ORIGIN || url.pathname !== "/" || url.search ||
            url.username || url.password || url.port) return null;
        const locator = documentLocatorFromHash(url.hash);
        return locator && locator.machine !== "this-mac" ? url.href : null;
    } catch {
        return null;
    }
}
export function resolveBoardSession(sessions, conversationID, machine = null) {
    const wanted = conversationKey(conversationID);
    const matches = (sessions || []).filter((row) => conversationKey(row.sessionId) === wanted
        && (!machine || (row.machine || row.identity?.machine) === machine));
    return matches.length === 1
        ? { id: matches[0].id }
        : {
              error: matches.length ? "session_ambiguous" : "session_unavailable"
          };
}

function conversationKey(value) {
    const text = String(value || "");
    return UUID_PATTERN.test(text) ? text.toLowerCase() : text;
}

export function boardSessionGroups(item, sessions, machine = null) {
    const links = (item && item.links || []).filter(row => row.kind === "session"
        && typeof row.targetId === "string" && row.targetId.length);
    const unique = new Map();
    links.forEach(link => {
        const key = conversationKey(link.targetId);
        if (!unique.has(key)) unique.set(key, {
            conversationId: link.targetId, label: link.label || link.targetId, receipts: []
        });
        unique.get(key).receipts.push(link);
    });
    const rows = [...unique.values()].map(record => {
        const key = conversationKey(record.conversationId);
        const live = (sessions || []).filter(session =>
            conversationKey(session.sessionId) === key
                && (!machine || (session.machine || session.identity?.machine) === machine));
        const owner = String(item && item.owner || "");
        return {
            ...record,
            live,
            activity: live.length > 1 ? "ambiguous" : live.length === 1 ? "live" : "history",
            role: owner && (conversationKey(owner) === key || live.some(row => row.id === owner))
                ? "owner" : "participant"
        };
    });
    const owner = rows.filter(row => row.role === "owner");
    return {
        owner,
        participants: rows.filter(row => row.role === "participant" && row.activity === "live"),
        history: rows.filter(row => row.activity === "history"),
        ambiguous: rows.filter(row => row.activity === "ambiguous"),
        ownerMissing: !!(item && item.owner) && owner.length === 0,
        receiptCount: links.length,
        conversationCount: rows.length
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
export function boardGroup(item) {
    if (item.type === "coordination") return "coordination";
    const group = item.progress && item.progress.group;
    if (["active", "waiting", "history", "completed", "canceled"].includes(group)) return group;
    const state = boardProgress(item);
    if (state === "landed" || state === "settled") return "completed";
    if (state === "canceled") return "canceled";
    if (state === "queued" || state === "verified") return "waiting";
    if (item.progress && item.progress.active === true) return "active";
    if (item.progress && item.progress.historical === true) return "history";
    return ["execution", "correction", "review_testing"].includes(state) ? "active" : "waiting";
}
function needsAttention(item) {
    const evidence = item.progress?.evidenceCounts;
    return item.state === "blocked" || boardProgress(item) === "blocked"
        || evidence?.blockingFindings > 0 || evidence?.failedVerifications > 0
        || item.remainingWork?.userDecisionCount > 0
        || (item.obligations || []).some(row => !row.resolved
            && (row.blocking || row.actorKind === "user"));
}
function planningOnly(item) {
    // Presentation partition only: never infer readiness from age, title, or parentage.
    // Explicit activity and unresolved attention take precedence over a planning label.
    return boardGroup(item) === "waiting" && boardProgress(item) === "planning"
        && item.progress?.active !== true && !(item.progress?.attemptCounts?.active > 0)
        // Old compact servers omit obligations; absence cannot establish no current attention.
        && Array.isArray(item.obligations) && !(item.projection?.obligations?.omittedCount > 0)
        && !needsAttention(item);
}
const LIST_GROUPS = ["active", "planning", "waiting", "history", "completed", "canceled", "coordination"];
function listGroup(item) {
    const model = item.listSummary;
    if (model?.coverage === "complete" && LIST_GROUPS.includes(model.group)) return model.group;
    const group = boardGroup(item);
    if (["active", "completed", "canceled", "coordination"].includes(group)) return group;
    if (needsAttention(item)) return "waiting";
    return planningOnly(item) ? "planning" : group;
}
function say(ctx, item) {
    if (item.type === "coordination")
        return words(
                ctx,
                "Coordination is recorded by time period or handoff, not completion status.",
                "依時段或交接記錄協調，不套用完成狀態。"
        );
    if (((item.progress && item.progress.basisCodes) || []).includes("active_delivery_lanes"))
        return words(ctx, "Related implementation work is active; see each Project below.", "相關實作正在進行；下方可查看各專案的進度。");
    if (boardProgress(item) === "queued" && item.progress && item.progress.attemptCounts && item.progress.attemptCounts.succeeded > 0)
        return words(ctx, "Earlier work produced output. The next step is queued.", "先前已有工作產出，目前等待下一步執行。");
    if (((item.progress && item.progress.basisCodes) || []).some(code => /^declared_.+_span$/.test(code)))
        return words(ctx,
            "A Session reports this activity; execution has not yet been confirmed by the dispatcher.",
            "Session 回報此階段正在進行；尚未取得派工系統的執行確認。");
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
function typeMark(ctx, parent, item) {
    const type = TYPES[item.type] ? item.type : "task";
    const mark = el(ctx, parent, "span", null, "board-kind board-kind-" + type);
    const tile = el(ctx, mark, "span", null, "board-kind-icon");
    const svg = ctx.doc.createElementNS("http://www.w3.org/2000/svg", "svg");
    for (const [name, value] of Object.entries({ viewBox: "0 0 24 24", width: "22", height: "22",
        fill: "none", stroke: "currentColor", "stroke-width": "1.6", "stroke-linecap": "round",
        "stroke-linejoin": "round", "aria-hidden": "true", focusable: "false" })) svg.setAttribute(name, value);
    const path = ctx.doc.createElementNS("http://www.w3.org/2000/svg", "path");
    path.setAttribute("d", TYPE_ICONS[type]);
    svg.appendChild(path);
    tile.appendChild(svg);
    el(ctx, mark, "span", localized(ctx, TYPES[type]), "board-kind-label");
    return mark;
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
    // Delivery is an attempt receipt, not a fifth lifecycle state or proof that execution,
    // verification and landing all happened. Show the known observation explicitly rather
    // than four dark dots that incorrectly suggest this work never began.
    if (step === undefined) {
        const checkpoint = el(ctx, parent, "div", null, "board-progress-checkpoint");
        el(ctx, checkpoint, "span", state === "canceled" ? "—" : "●", "board-step-dot");
        el(ctx, checkpoint, "span", localized(ctx, STATUS[state]));
        return;
    }
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
    typeMark(ctx, top, item);
    badge(ctx, top, item);
    const narrative = reading(ctx, item);
    el(ctx, node, "strong", shortTitle(narrative.title || item.key || item.id), "board-card-title");
    if (narrative.summary) el(ctx, node, "span", narrative.summary, "board-card-summary");
    const blocker = (item.obligations || []).find((row) => row.blocking && !row.resolved);
    el(ctx, node, "span", blocker ? blocker.title : say(ctx, item), "board-card-next");
    journey(ctx, node, item);
    const foot = el(ctx, node, "span", null, "board-card-facts"),
        checks = (item.checklist || []).filter((row) => row.required),
        summary = item.cardSummary && item.cardSummary.checklist,
        total = summary ? summary.required : checks.length,
        completed = summary ? summary.requiredCompleted : checks.filter(row => ["passed", "not_applicable"].includes(row.status)).length;
    if (total && item.type !== "coordination")
        el(
            ctx,
            foot,
            "span",
            (summary && summary.coverage === "partial" ? words(ctx, "Loaded ", "已載入 ") : "") + completed +
                "/" +
                total +
                words(ctx, " checks", " 項檢查")
        );
    if (item.owner && !/[a-f0-9]{8}-[a-f0-9-]{20,}/i.test(item.owner))
        el(ctx, foot, "span", words(ctx, "Owner ", "負責 ") + item.owner);
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
function historicalCards(ctx, part, rows, searching) {
    if (searching) part.open = true;
    let built = false;
    const key = ctx.state.projectId + ":" + part.dataset.boardSection;
    ctx.cardLimits ||= new Map();
    const fill = () => {
        if (!part.open || built) return;
        built = true;
        const list = el(ctx, part, "div", null, "board-card-grid");
        let drawn = 0, more;
        const append = () => {
            if (more) part.removeChild(more);
            const limit = Math.min(rows.length, Math.max(30, ctx.cardLimits.get(key) || 30));
            rows.slice(drawn, limit).forEach(row => list.appendChild(card(ctx, row)));
            drawn = limit;
            if (drawn < rows.length) more = button(ctx, part,
                words(ctx, "Show more", "顯示更多") + " · " + drawn + "/" + rows.length,
                "more-history", () => { ctx.cardLimits.set(key, drawn + 30); append(); });
        };
        append();
    };
    part.addEventListener("toggle", fill);
    fill();
}
function fact(ctx, parent, label, value) {
    const row = el(ctx, parent, "div", null, "board-fact");
    el(ctx, row, "span", label, "board-fact-label");
    el(ctx, row, "span", value == null || value === "" ? "—" : value, "board-fact-value");
}
export function renderCompletionReport(ctx, parent, item, showHistory = true) {
    const report = item.completionReport || { status: "absent" },
        coordination = item.type === "coordination",
        title = report.status === "superseded"
            ? words(ctx, "Report version v" + report.version, "報告版本 v" + report.version)
            : coordination
            ? words(ctx, "Period / handoff report", "階段／交接報告")
            : words(ctx, "Completion report", "結案報告"),
        container = section(ctx, parent, title, report.status === "absent", "completion-report");
    container.className += " board-completion-report";
    if (report.status === "absent") {
        el(
            ctx,
            container,
            "p",
            coordination
                ? words(
                      ctx,
                      "No period or handoff report has been recorded. This does not assign a completion status.",
                      "尚未記錄階段或交接報告；這不代表任何完成狀態。"
                  )
                : words(
                      ctx,
                      "No completion report has been recorded. Status and evidence remain available separately.",
                      "尚未記錄結案報告；狀態與證據仍會分開呈現。"
                  ),
            "board-report-empty"
        );
        return container;
    }
    const historical = report.status === "historical_needs_update",
        superseded = report.status === "superseded";
    el(
        ctx,
        container,
        "p",
        historical
            ? words(ctx, "Earlier scope · needs an updated report", "較早範圍・需要更新報告")
            : superseded
              ? words(ctx, "Superseded report · retained history", "已被取代的報告・保留歷史")
            : words(ctx, "Current recorded scope", "目前記錄範圍"),
        "board-report-scope" + (historical || superseded ? " is-historical" : "")
    );
    el(
        ctx,
        container,
        "p",
        words(
            ctx,
            "Attributed narrative · not verification or landing evidence",
            "具名敘述・不等同驗證或落地證據"
        ),
        "board-report-authority"
    );
    const provenance = [
        report.actor,
        report.authorship === "assistant"
            ? words(ctx, "AI-assisted", "AI 協作")
            : report.authorship === "human"
              ? words(ctx, "Human-authored", "人工撰寫")
              : null,
        report.model,
        report.authoredAt ? date(report.authoredAt) : null,
        report.version ? "v" + report.version : null
    ].filter(Boolean);
    if (provenance.length)
        el(ctx, container, "p", provenance.join(" · "), "board-report-provenance");
    const boundary = report.reportBoundary;
    if (boundary) {
        const value = boundary.kind === "time_interval"
            ? boundary.label + " · " + date(boundary.startedAt) + " – " + date(boundary.endedAt)
            : boundary.label + words(ctx, " · same-item handoff", "・同項目交接");
        el(ctx, container, "p", value, "board-report-boundary");
    }
    const body = el(ctx, container, "div", null, "board-report-body");
    [
        ["objective", "Objective", "目標"],
        ["deliveredOutcomes", "Delivered outcomes", "交付成果"],
        ["verificationLanding", "Verification & landing", "驗證與落地"],
        ["remainingWork", "Remaining work", "尚待處理"],
        ["lessons", "Lessons", "經驗與學習"]
    ].forEach(([key, en, zh]) => {
        const part = el(ctx, body, "section", null, "board-report-part");
        el(ctx, part, "h3", words(ctx, en, zh), "board-report-part-title");
        el(
            ctx,
            part,
            "p",
            report[key] || words(ctx, "Not recorded.", "尚未記錄。"),
            "board-report-text"
        );
    });
    const sources = report.sourceReferences || [];
    if (sources.length) {
        const sourceBlock = el(ctx, container, "div", null, "board-report-sources");
        el(ctx, sourceBlock, "h3", words(ctx, "Sources", "來源"), "board-report-part-title");
        sources.forEach((row) => {
            const url = safeURL(row.url),
                line = el(ctx, sourceBlock, "div", null, "board-report-source"),
                label = row.label || row.targetId || words(ctx, "Unlabelled source", "未命名來源"),
                link = el(ctx, line, url ? "a" : "span", label, "board-report-source-link");
            if (url) {
                link.href = url;
                link.target = "_blank";
                link.rel = "noopener noreferrer";
            }
            const relationship = row.relationship || (row.resolution || "unresolved") + "_at_authorship";
            let relation = relationship === "unresolved_at_authorship"
                ? words(ctx, "Unresolved at authorship", "撰寫時尚未解析")
                : relationship === "same_project_at_authorship"
                  ? words(ctx, "Same Project at authorship", "撰寫時屬同一 Project")
                  : words(ctx, "Same item at authorship", "撰寫時屬同一項目");
            if (row.resolvedAt) relation += " · " + date(row.resolvedAt);
            el(
                ctx,
                line,
                "span",
                relation,
                "board-report-source-state"
            );
        });
    }
    const history = Array.isArray(item.completionReportHistory)
        ? item.completionReportHistory : [];
    if (showHistory && history.length) {
        const historyBlock = el(ctx, container, "div", null, "board-report-history");
        el(ctx, historyBlock, "h3", words(ctx, "Earlier versions", "較早版本"),
            "board-report-part-title");
        const actions = el(ctx, historyBlock, "div", null, "board-report-history-actions");
        history.forEach((row) => {
            const action = button(ctx, actions, "v" + row.version + " · " + date(row.authoredAt),
                "report-version", () => ctx.openReport(row.id), "board-report-history-button");
            action.dataset.boardReportId = row.id;
        });
        const selected = ctx.state.reportSelection;
        if (selected) {
            const reader = el(ctx, historyBlock, "div", null, "board-report-reader");
            reader.setAttribute("aria-live", "polite");
            if (selected.status === "loading") {
                el(ctx, reader, "p", words(ctx, "Loading report version…", "正在載入報告版本…"),
                    "board-report-empty");
            } else if (selected.status === "error") {
                el(ctx, reader, "p",
                    words(ctx, "This report version could not be loaded. ", "無法載入這個報告版本。")
                        + (selected.error || ""), "board-report-empty");
                button(ctx, reader, words(ctx, "Retry version", "重試此版本"), "report-retry",
                    () => ctx.openReport(selected.id), "board-report-history-button");
            } else if (selected.report) {
                renderCompletionReport(ctx, reader,
                    { ...item, completionReport: selected.report, completionReportHistory: [] }, false);
            }
        }
    }
    return container;
}
function remainingStatus(ctx, row) {
    const names = {
        todo: ["Not started", "尚未開始"], doing: ["In progress", "進行中"],
        failed: ["Needs correction", "待修正"], waiting: ["Waiting", "等待中"],
        canceled: ["Canceled", "已取消"], unknown: ["Unknown", "未知"],
        planning: ["Planning", "規劃中"], execution: ["In progress", "執行中"],
        review_testing: ["Review & testing", "審查與測試"], correction: ["Correction", "修正中"],
        verified: ["Verified", "已驗證"], delivered: ["Delivered only", "僅已交付"],
        landed: ["Landed", "已落地"], blocked: ["Blocked", "受阻"]
    };
    return localized(ctx, names[row.status] || names.unknown);
}
function remainingDisposition(ctx, row) {
    const names = {
        required: ["Required", "必要"], optional: ["Optional", "可選"],
        canceled: ["Canceled", "已取消"], unknown: ["Unknown", "未知"]
    };
    return localized(ctx, names[row.disposition] || names.required);
}
function remainingRow(ctx, parent, row, item) {
    const line = row.kind === "child"
        ? button(ctx, parent, null, "remaining-child", () =>
            ctx.openProjectItem(row.projectId || item.projectId, row.id),
            "board-remaining-row board-remaining-child")
        : el(ctx, parent, "div", null, "board-remaining-row");
    const copy = el(ctx, line, "div", null, "board-remaining-copy");
    el(ctx, copy, "strong", row.title, "board-remaining-title");
    if (row.requiredAction && row.requiredAction !== row.title)
        el(ctx, copy, "span", row.requiredAction, "board-remaining-action");
    const meta = [remainingStatus(ctx, row), remainingDisposition(ctx, row), row.owner].filter(Boolean);
    if (row.actorKind === "unknown")
        meta.push(words(ctx, "Owner kind unknown", "負責者類型未知"));
    el(ctx, line, "span", meta.join(" · "), "board-remaining-meta");
    return line;
}
function collectionSelection(item, kind, offset) {
    return "collection:" + item + ":" + kind + ":" + offset;
}
function supplementScope(value) {
    const match = /^workflow_run=(run-[0-9a-f]{32});session=([^;]{1,200});disposition=(required|optional)$/.exec(
        String(value || ""));
    return match ? { run: match[1], session: match[2], disposition: match[3] } : null;
}
export function mergeBoardRemainingWork(item) {
    let remainder = item && item.remainingWork;
    if (!remainder && item) {
        const pendingChecks = (item.checklist || [])
            .filter(row => !["passed", "not_applicable", "canceled"].includes(row.status))
            .map(row => ({ ...row, kind: "checklist",
                disposition: row.required ? "required" : "optional" }));
        const pendingObligations = (item.obligations || []).filter(row => !row.resolved)
            .map(row => ({ ...row, kind: "obligation", status: row.status || "waiting",
                disposition: row.disposition || (row.blocking ? "required" : "optional"),
                actorKind: row.actorKind || "unknown" }));
        remainder = {
            work: pendingChecks.concat(pendingObligations.filter(row => row.actorKind !== "user")),
            userDecisions: pendingObligations.filter(row => row.actorKind === "user"),
            workOmittedCount: 0, userDecisionOmittedCount: 0
        };
    }
    if (!remainder) return remainder;
    const work = (remainder.work || []).map(row => ({ ...row }));
    const checklists = work.filter(row => row.kind === "checklist");
    const obligations = work.filter(row => row.kind === "obligation");
    const consumed = new Set();
    checklists.forEach(checklist => {
        const relation = typeof checklist.supplementRelationId === "string"
            && /^wfs-[0-9a-f]{64}$/.test(checklist.supplementRelationId)
            ? checklist.supplementRelationId : null;
        if (!relation) return;
        const sameChecks = checklists.filter(row => row.supplementRelationId === relation);
        const candidates = obligations.filter(row => row.supplementRelationId === relation);
        if (sameChecks.length !== 1 || candidates.length !== 1) return;
        const obligation = candidates[0], scope = supplementScope(obligation.blockingScope);
        if (!scope || scope.disposition !== checklist.disposition
            || obligation.owner !== scope.session || obligation.actorKind === "user") return;
        checklist.requiredAction = obligation.requiredAction || checklist.requiredAction;
        checklist.actorKind = obligation.actorKind;
        checklist.sources = [
            { kind: "checklist", id: checklist.id, supplementRelationId: relation },
            { kind: "obligation", id: obligation.id, workflowRun: scope.run,
              session: scope.session, supplementRelationId: relation }
        ];
        consumed.add(obligation.id);
    });
    return { ...remainder, work: work.filter(row =>
        !(row.kind === "obligation" && consumed.has(row.id))) };
}
function loadMore(ctx, parent, kind, count, omitted) {
    if (!(omitted > 0)) return;
    const control = button(ctx, parent,
        words(ctx, "Load " + omitted + " more", "載入其餘 " + omitted + " 筆"),
        "load-more", () => ctx.loadCollection(kind, count), "board-load-more");
    control.setAttribute("aria-label", words(ctx,
        "Load more retained Board records", "載入更多已保留的看板紀錄"));
}
function renderRemainingWork(ctx, parent, item) {
    const remainder = mergeBoardRemainingWork(item);
    if (!remainder) return;
    const work = remainder.work || [], decisions = remainder.userDecisions || [];
    if (!work.length && !decisions.length) return;
    const container = section(ctx, parent, words(ctx, "What remains", "還欠什麼"), false,
        "remaining-work");
    container.className += " board-remaining-work";
    const columns = el(ctx, container, "div", null, "board-remaining-columns");
    const workPart = el(ctx, columns, "section", null, "board-remaining-group");
    el(ctx, workPart, "h3", words(ctx, "What we will do next", "接下來由我們處理"),
        "board-remaining-group-title");
    if (work.length) work.forEach(row => remainingRow(ctx, workPart, row, item));
    else el(ctx, workPart, "p", words(ctx, "No recorded work remains.", "目前沒有記錄中的後續工作。"),
        "board-section-help");
    loadMore(ctx, workPart, "remaining_work", work.length,
        remainder.workOmittedCount || 0);
    const decisionPart = el(ctx, columns, "section", null,
        "board-remaining-group board-user-decisions");
    el(ctx, decisionPart, "h3", words(ctx, "What you need to decide", "需要你決定"),
        "board-remaining-group-title");
    if (decisions.length) decisions.forEach(row => remainingRow(ctx, decisionPart, row, item));
    else el(ctx, decisionPart, "p", words(ctx, "No decision is waiting on you.", "目前沒有等待你決定的事項。"),
        "board-section-help");
    loadMore(ctx, decisionPart, "user_decisions", decisions.length,
        remainder.userDecisionOmittedCount || 0);
}
function renderDocumentReferences(ctx, parent, item) {
    const documents = item.documentReferences || [];
    const groups = [
        ["plan", "Original plan", "原始規劃"],
        ["decision", "Decisions & changes", "決策與變更"],
        ["reference", "Reference documents", "參考文件"]
    ];
    groups.forEach(([purpose, en, zh]) => {
        const rows = documents.filter(row => row.purpose === purpose);
        if (!rows.length) return;
        const part = section(ctx, parent, words(ctx, en, zh));
        rows.slice().sort((a, b) => (a.status === "current" ? -1 : 1)
            - (b.status === "current" ? -1 : 1) || (b.version || 0) - (a.version || 0))
            .forEach(row => {
                const line = el(ctx, part, "div", null, "board-document-reference"),
                    url = canonicalDocumentURL(row.url),
                    link = el(ctx, line, url ? "a" : "span", row.title, "board-document-link");
                if (url) {
                    link.href = url;
                    link.target = "_blank";
                    link.rel = "noopener noreferrer";
                }
                el(ctx, line, "span",
                    (row.status === "superseded"
                        ? words(ctx, "Earlier version", "較早版本")
                        : words(ctx, "Current", "目前版本")) + " · v" + row.version,
                    "board-document-version");
            });
        el(ctx, part, "p", words(ctx,
            "Narrative reference only; content opens when selected and does not establish acceptance.",
            "僅為敘事參照；選取後才讀取內容，也不構成驗收。"), "board-section-help");
    });
    const projection = item.projection && item.projection.documentReferences;
    loadMore(ctx, parent, "document_references", documents.length,
        projection && projection.omittedCount || 0);
}
function openBoardConversation(ctx, item, record) {
    return Promise.resolve().then(() => ctx.env.openSession && ctx.env.openSession(
        record.conversationId,
        ctx.state.projects.find(project => project.id === item.projectId)
            || ctx.state.projectPresentation,
        ctx.state.machine
    )).then(answer => {
        if (answer && answer.error)
            ctx.status(words(ctx,
                "This conversation has no unique Session to open. Nothing was resumed.",
                "這段對話目前沒有可唯一開啟的 Session；未恢復任何對話。"), true);
    }).catch(error => ctx.status(error.message || String(error), true));
}
function renderSessionOwnership(ctx, parent, item) {
    const groups = boardSessionGroups(item,
        ctx.env.sessions ? ctx.env.sessions() : [], ctx.state.machine);
    const part = section(ctx, parent, words(ctx, "Owner & how to continue", "負責人與如何繼續"));
    fact(ctx, part, words(ctx, "Recorded owner", "目前負責人"),
        item.owner || words(ctx, "Not assigned", "尚未指派"));
    const draw = (record, group) => {
        const row = el(ctx, part, "div", null, "board-session-row");
        row.dataset.sessionGroup = group;
        const labels = record.activity === "live"
            ? ["Open live Session", "開啟執行中 Session"]
            : record.activity === "ambiguous"
                ? ["Review matching Sessions", "檢查多個符合的 Session"]
                : ["Review history / resume", "查看歷史／確認繼續"];
        button(ctx, row, words(ctx, ...labels), "open-session",
            () => openBoardConversation(ctx, item, record));
        el(ctx, row, "span", record.label, "board-session-label");
    };
    groups.owner.forEach(row => draw(row, "owner"));
    groups.participants.forEach(row => draw(row, "participant-live"));
    groups.history.filter(row => row.role !== "owner").forEach(row => draw(row, "history"));
    groups.ambiguous.filter(row => row.role !== "owner").forEach(row => draw(row, "ambiguous"));
    if (groups.ownerMissing)
        el(ctx, part, "p", words(ctx,
            "The recorded owner has no exact conversation identity in this item.",
            "目前負責人沒有可由本項目精確對應的 conversation identity。"), "board-section-help");
    if (!groups.conversationCount)
        el(ctx, part, "p", words(ctx,
            "No responsible Session is linked yet.", "尚未連結負責 Session。"),
            "board-section-help");
    el(ctx, part, "p", words(ctx,
        "Direct assignment is not available here. Open the candidate Session and let that Session confirm takeover; this view never sends or starts work by itself.",
        "此處尚未提供可安全確認的直接指派。請開啟候選 Session，由該 Session 確認接手；本畫面不會自行送訊息或啟動工作。"),
        "board-section-help board-handoff-gap");
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
    const identity = el(ctx, head, "div", null, "board-card-top");
    typeMark(ctx, identity, item);
    badge(ctx, identity, item);
    const narrative = reading(ctx, item);
    el(ctx, head, "h2", narrative.title || item.title, "board-detail-title");
    button(ctx, head, words(ctx, "Copy item link", "複製項目連結"),
        "copy-item-link", () => ctx.copyLink(item.id), "board-button board-copy-link");
    if (narrative.summary) el(ctx, head, "p", narrative.summary, "board-detail-summary");
    if (narrative.locale) el(ctx, head, "p", words(ctx, "AI reading summary · progress follows recorded evidence", "AI 整理・進度仍以實際紀錄為準"), "board-narrative-provenance");
    journey(ctx, head, item);
    el(ctx, head, "p", say(ctx, item), "board-detail-summary");
    renderSessionOwnership(ctx, target, item);
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
    renderRemainingWork(ctx, target, item);
    renderCompletionReport(ctx, target, item);
    renderDocumentReferences(ctx, target, item);
    if (narrative.outcome && (!item.completionReport || item.completionReport.status === "absent")) {
        const outcomes = section(ctx, target, words(ctx, "What changed", "完成了什麼"));
        el(ctx, outcomes, "p", narrative.outcome, "board-detail-summary");
    }
    if (narrative.nextStep) {
        const next = section(ctx, target, words(ctx, "Recorded next step", "紀錄中的下一步"));
        el(ctx, next, "p", narrative.nextStep, "board-detail-summary");
    }
    if ((item.deliveryLanes || []).length) {
        const lanes = section(ctx, target, words(ctx, "Delivery across Projects", "各專案的實作進度"));
        item.deliveryLanes.forEach(lane => {
            const row = button(ctx, lanes, null, "delivery-lane", () => ctx.openProjectItem(lane.projectId, lane.id), "board-delivery-lane");
            el(ctx, row, "span", lane.projectName, "board-lane-project");
            el(ctx, row, "strong", shortTitle(reading(ctx, lane).title || lane.title));
            badge(ctx, row, lane);
            el(ctx, row, "span", [lane.owner, lane.checklistTotal ? lane.checklistDone + "/" + lane.checklistTotal + words(ctx, " checks", " 項檢查") : null].filter(Boolean).join(" · "), "board-card-facts");
        });
        if (item.deliveryLaneCount > item.deliveryLanes.length)
            el(ctx, lanes, "p", words(ctx, "More related work exists in its owning Projects.", "還有其他相關項目，可至所屬專案查看。"), "board-section-help");
    }
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
    if (item.type !== "coordination" && (item.checklist || []).length) {
        const part = section(ctx, target, words(ctx, "Completed checks", "已完成檢查"));
        const names = {
            todo: ["Not started", "尚未開始"],
            doing: ["In progress", "進行中"],
            passed: ["Passed", "通過"],
            failed: ["Needs correction", "待修正"],
            not_applicable: ["Not applicable", "不適用"]
        };
        const completed = item.checklist.filter(row =>
            ["passed", "not_applicable"].includes(row.status));
        completed.forEach((row) => {
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
        if (!completed.length)
            el(ctx, part, "p", words(ctx,
                "No checks are complete yet; remaining checks are listed above.",
                "尚無已完成檢查；未完成項目已列在上方。"), "board-section-help");
        else if (completed.length < item.checklist.length)
            el(ctx, part, "p", words(ctx,
                "Checks still awaiting work or acceptance are listed above.",
                "仍待處理或驗收的檢查已列在上方。"), "board-section-help");
    }
    if (["feature", "refactor", "epic"].includes(item.type) && (item.milestones || []).length) {
        const part = section(ctx, target, words(ctx, "Milestones", "里程碑"));
        item.milestones.forEach((row) =>
            fact(ctx, part, row.title, ["passed", "not_applicable"].includes(row.status) ? "✓" : "○")
        );
    }
    if ((item.artifacts || []).length) {
        const part = section(ctx, target, words(ctx, "Outputs", "交付成果"));
        item.artifacts.forEach((row) => {
            const url = safeURL(row.url),
                link = el(ctx, part, url ? "a" : "span", row.title || row.url,
                    "board-artifact-link board-output-link");
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
    const records = section(ctx, target, words(ctx, "Technical records", "技術紀錄"), true);
    (item.checklist || []).forEach(row => fact(ctx, records,
        words(ctx, "Checklist source", "Checklist 原始來源"),
        (row.id || "(no id)") + " · " + row.title + " · " + row.status
            + (row.supplementRelationId ? " · " + row.supplementRelationId : "")));
    (item.obligations || []).forEach(row => fact(ctx, records,
        words(ctx, "Obligation source", "Obligation 原始來源"),
        (row.id || "(no id)") + " · " + row.title + " · "
            + (row.blockingScope || row.owner || "unknown")
            + (row.supplementRelationId ? " · " + row.supplementRelationId : "")));
    (item.links || []).filter(row => row.kind !== "session").forEach((row) => {
        const line = el(ctx, records, "div", null, "board-link-row");
        line.dataset.boardLinkKind = row.kind;
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
    const sessionReceipts = boardSessionGroups(item,
        ctx.env.sessions ? ctx.env.sessions() : [], ctx.state.machine);
    const receiptRows = new Map();
    [...sessionReceipts.owner, ...sessionReceipts.participants,
     ...sessionReceipts.history, ...sessionReceipts.ambiguous].forEach(row =>
        receiptRows.set(conversationKey(row.conversationId), row));
    receiptRows.forEach(row => fact(
        ctx, records, words(ctx, "Conversation receipts", "Conversation 原始來源"),
        row.conversationId + " · " + row.receipts.map(receipt => receipt.id || "(no id)").join(", ")
    ));
    (item.spans || []).forEach((row) =>
        fact(
            ctx,
            records,
            localized(ctx, PHASES[row.phase] || [row.phase, row.phase]),
            date(row.startedAt) + " – " + (row.endedAt ? date(row.endedAt) : words(ctx, "Ongoing", "持續中"))
        )
    );
    const original = section(ctx, target, words(ctx, "Original objective", "原始目標與描述"), true);
    fact(ctx, original, words(ctx, "Original title", "原始標題"), item.title);
    fact(ctx, original, words(ctx, "Original description", "原始描述"), item.summary || words(ctx, "Not recorded", "尚未記錄"));
    const proof = section(ctx, target, words(ctx, "Why this status?", "狀態的依據"), true),
        current = item.currentEvidence || {};
    if (item.progress && item.progress.reason) {
        fact(ctx, proof, words(ctx, "Basis", "判讀依據"), say(ctx, item));
        const raw = section(ctx, proof, words(ctx, "Technical source", "技術來源"), true);
        fact(ctx, raw, "Source reason", item.progress.reason);
        fact(ctx, raw, words(ctx, "Original title", "原始標題"), item.title);
        fact(ctx, raw, "ID", item.id);
    }
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
        machine: null,
        locatorBacked: false,
        routeError: null,
        projectPresentation: null,
        selectionLoaded: false,
        readStatus: "loading",
        itemId: null,
        revision: -1,
        enabled: true,
        active: false,
        readTicket: 0,
        reportTicket: 0,
        collectionTickets: {},
        reportSelection: null
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
    ctx.status = (message, error = false, retry = true) => {
        clear(elements["board-status"]);
        elements["board-status"].className = "board-status" + (error ? " board-status-error" : "");
        el(ctx, elements["board-status"], "span", message);
        if (error && retry)
            button(ctx, elements["board-status"], words(ctx, "Retry", "重試"), "retry", refresh);
    };
    ctx.copyLink = (item) => {
        const url = boardShareURL(state.machine, state.projectId, item);
        if (!url || !environment.copy) {
            ctx.status(words(ctx,
                "A current Cloud machine is required before this Board link can be shared.",
                "必須先確認目前的 Cloud machine，才能分享這個看板連結。"), true, false);
            return Promise.resolve({ error: "board_machine_missing" });
        }
        return Promise.resolve(environment.copy(url)).then(() => {
            ctx.status(words(ctx, "Board link copied.", "已複製看板連結。"));
            return { ok: true, url };
        }).catch(error => {
            ctx.status((error && error.message) || String(error), true);
            return { error: "copy_failed" };
        });
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
        if (elements["board-timeline-tab"])
            elements["board-timeline-tab"].textContent = words(ctx, "Timeline", "時間軸");
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
        const share = el(ctx, target, "div", null, "board-share-row");
        button(ctx, share, words(ctx, "Copy project link", "複製專案連結"),
            "copy-project-link", () => ctx.copyLink(null));
        const all = state.items.filter((row) => row.projectId === state.projectId),
            query = elements["board-search"].value.trim().toLocaleLowerCase();
        const rows = all.filter(
            (row) =>
                !query ||
                [row.title, row.summary, reading(ctx, row).title, reading(ctx, row).summary, row.key, row.owner].join(" ").toLocaleLowerCase().includes(query)
        );
        const delivery = all.filter((row) => row.type !== "coordination"),
            coordinated = rows.filter((row) => row.type === "coordination");
        const active = rows.filter(row => listGroup(row) === "active"),
            planned = rows.filter(row => listGroup(row) === "planning"),
            waiting = rows.filter(row => listGroup(row) === "waiting"),
            unconfirmed = rows.filter(row => listGroup(row) === "history"),
            history = rows.filter(row => ["completed", "canceled"].includes(listGroup(row)));
        const overview = el(ctx, target, "div", null, "board-overview");
        const summary = project && project.summary;
        const counts = summary?.listGroups;
        const completeCounts = project?.summaryCoverage?.status === "complete"
            && LIST_GROUPS.every(key => Number.isSafeInteger(counts?.[key]) && counts[key] >= 0);
        const modelCount = (key, loaded) => summary && Number.isInteger(summary[key]) && summary[key] >= 0
            ? summary[key] : loaded;
        const stats = completeCounts ? [
            [counts.active, "Active now", "正在進行"],
            [counts.waiting, "Ready or needs attention", "等待推進／需要處理"],
            [counts.planning, "Planning · not started", "規劃・尚未開始"],
            [modelCount("landed", 0), "Landed", "已落地"]
        ] : [
            [
                modelCount("active", delivery.filter(row => boardGroup(row) === "active").length),
                "Active now",
                "正在進行"
            ],
            [
                modelCount("waiting", delivery.filter(row => boardGroup(row) === "waiting").length),
                "Waiting / planned",
                "等待／規劃"
            ],
            [modelCount("landed", delivery.filter((row) => boardProgress(row) === "landed").length), "Landed", "已落地"]
        ];
        stats.forEach(([count, en, chinese]) => {
            const stat = el(ctx, overview, "div", null, "board-overview-stat");
            el(ctx, stat, "strong", count);
            el(ctx, stat, "span", words(ctx, en, chinese));
        });
        const total = Number.isSafeInteger(project?.itemCount) && project.itemCount >= all.length
            ? project.itemCount : null;
        el(ctx, target, "p", words(ctx,
            `Loaded ${all.length}${total === null ? " / total unknown" : ` / ${total}`} items`,
            `已載入 ${all.length}${total === null ? " / 總數未提供" : ` / ${total}`} 項`)
            + (query ? words(ctx, ` · ${rows.length} search matches (loaded items only)`,
                ` · 搜尋符合 ${rows.length} 項（僅搜尋已載入）`) : ""), "board-section-help");
        const sectionCount = (group, loaded) => words(ctx, ` · ${loaded} ${query ? "matches" : "loaded"}`,
            ` · ${loaded} ${query ? "項符合搜尋" : "項已載入"}`)
            + (completeCounts ? words(ctx, ` / ${counts[group]} total`, ` / 共 ${counts[group]} 項`) : "");
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
                    : modelCount("active", 0) > 0 ? words(ctx,
                          "More work is recorded than this view has loaded.",
                          "專案還有進行中的工作，此畫面尚未載入。")
                      : !unconfirmed.length && modelCount("history", 0) > 0
                      ? words(ctx, "Some historical records are not loaded. This does not mean they are still active.", "部分歷史紀錄尚未載入，不代表仍在進行。")
                      : waiting.length || planned.length || unconfirmed.length || modelCount("waiting", 0) > 0
                      ? words(ctx, "No work is executing right now. Other records are shown below.", "目前沒有執行中的工作，其他待處理與歷史紀錄列於下方。") : words(
                          ctx,
                          "No open work. Tell your assistant what you would like to do next.",
                          "目前沒有待推進的項目。想做什麼，直接在對話中告訴 assistant。"
                      ),
                "board-empty"
            );
        if (waiting.length) {
            const part = section(ctx, target, words(ctx, "Ready or needs attention", "等待推進／需要處理")
                + sectionCount("waiting", waiting.length)),
                list = el(ctx, part, "div", null, "board-card-grid");
            waiting.forEach(row => list.appendChild(card(ctx, row)));
        }
        if (planned.length || (completeCounts && counts.planning > 0)) {
            const part = section(ctx, target,
                words(ctx, "Planning · not started", "規劃區・尚未開始") + sectionCount("planning", planned.length),
                true, "planned-work");
            el(ctx, part, "p", words(ctx,
                "Future work is kept here, separate from current execution. Expand to review plans and subtasks.",
                "還沒開始的計畫與子項目集中在這裡，與目前工作分開。展開即可查看。"), "board-section-help");
            historicalCards(ctx, part, planned, !!query);
            if (!planned.length) el(ctx, part, "p", query
                ? words(ctx, "No matching plans in loaded items.", "已載入項目沒有符合搜尋的計畫。")
                : words(ctx, "Plans exist but have not been loaded in this view.", "尚有計畫未載入此畫面。"), "board-section-help");
        }
        if (unconfirmed.length) {
            const part = section(ctx, target,
                words(ctx, "Historical records · outcome unconfirmed", "歷史紀錄・結果尚未確認")
                    + " · " + unconfirmed.length, true, "unconfirmed-history");
            part.className += " board-unconfirmed-history";
            el(ctx, part, "p", words(ctx,
                "No activity is currently recorded for these items. Missing outcome evidence does not mean work is ongoing.",
                "這些項目目前沒有執行活動紀錄。結果資料不足，不代表仍在進行。"), "board-section-help");
            historicalCards(ctx, part, unconfirmed, !!query);
        }
        if (history.length) {
            const part = section(
                ctx,
                target,
                words(ctx, "Past work", "歷史成果") + " · " + history.length,
                true,
                "delivery-history"
            );
            part.className += " board-history-group";
            historicalCards(ctx, part, history.sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0)), !!query);
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
            historicalCards(ctx, part, coordinated, !!query);
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
        if (state.routeError) return Promise.resolve({ error: "board_link_invalid" });
        const ticket = ++state.readTicket,
            project = state.projectId,
            item = state.itemId;
        if (!quiet) ctx.status(words(ctx, "Reading project records…", "正在讀取專案紀錄…"));
        const promise = Promise.resolve()
            .then(() => {
                if (!environment.read) throw new Error(words(ctx, "Board unavailable", "無法讀取看板"));
                return environment.read(
                    project || undefined, item || undefined, state.machine || undefined);
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
                if (state.enabled && state.readStatus === "stale") message = board.readState && board.readState.error && !board.readState.refreshing
                    ? words(ctx, "Update temporarily failed; retained records remain readable.", "更新暫時失敗；仍可閱讀先前保留的紀錄。")
                    : words(ctx, "Showing the last available records; updating in the background.", "目前顯示上次可用的紀錄，正在背景更新。");
                if (state.readStatus === "error") message = words(ctx,
                    "Project records are temporarily unavailable; retrying in the background.", "專案紀錄暫時無法取得，稍後會在背景重試。");
                if (board.source && board.source.observedAt) message += words(ctx, " · Records as of ", "・資料截至 ") + date(board.source.observedAt);
                if (board.truncated)
                    message += words(ctx, " · Some records are not loaded", "・部分紀錄未載入");
                const ingestion = board.source && board.source.ingestion;
                if (ingestion && ingestion.issueCount)
                    message += words(ctx, " · Some source records still need matching", "・部分來源紀錄尚待對應");
                ctx.status(message);
                elements["board-status"].title = [board.readState && "Snapshot " + board.readState.revision + " / " + board.revision,
                    ingestion && (ingestion.reasons || []).join(", ")].filter(Boolean).join(" · ");
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
    function loadReport(report) {
        const ticket = ++state.reportTicket,
            project = state.projectId,
            item = state.itemId;
        state.reportSelection = { id: report, status: "loading" };
        render();
        return Promise.resolve()
            .then(() => {
                if (!environment.read || !project || !item)
                    throw new Error(words(ctx, "Board unavailable", "無法讀取看板"));
                return environment.read(
                    project, boardReportSelection(item, report), state.machine || undefined);
            })
            .then((answer) => {
                if (!state.active || ticket !== state.reportTicket
                    || project !== state.projectId || item !== state.itemId) return;
                const board = answer && answer.board,
                    selected = board && board.reportSelection;
                if (!board || board.schemaVersion !== 1 || board.revision < state.revision
                    || !board.item || board.item.id !== item || board.item.projectId !== project
                    || !selected || selected.id !== report)
                    throw new Error(words(ctx, "Report response is stale or incomplete.",
                        "報告回應已過期或不完整。"));
                state.reportSelection = { id: report, status: "ready", report: selected };
                render();
            })
            .catch((error) => {
                if (!state.active || ticket !== state.reportTicket
                    || project !== state.projectId || item !== state.itemId) return;
                state.reportSelection = {
                    id: report, status: "error", error: error.message || String(error)
                };
                render();
            });
    }
    ctx.openReport = loadReport;
    ctx.loadCollection = function (kind, offset) {
        const project = state.projectId, item = state.itemId, selected = state.item,
            ticket = (state.collectionTickets[kind] || 0) + 1;
        state.collectionTickets[kind] = ticket;
        if (!environment.read || !project || !item || !selected) return Promise.resolve();
        return Promise.resolve()
            .then(() => environment.read(
                project, collectionSelection(item, kind, offset), state.machine || undefined))
            .then((answer) => {
                if (!state.active || ticket !== state.collectionTickets[kind]
                    || item !== state.itemId || selected !== state.item) return;
                const board = answer && answer.board, collection = board && board.collection;
                if (!board || typeof board.revision !== "number" || board.revision < state.revision
                    || !collection || collection.itemId !== item || collection.kind !== kind
                    || collection.offset !== offset || !Array.isArray(collection.rows))
                    throw new Error(words(ctx, "Board continuation is incomplete.",
                        "看板續頁回應不完整。"));
                if (kind === "document_references") {
                    selected.documentReferences = (selected.documentReferences || []).concat(collection.rows);
                    selected.projection ||= {};
                    selected.projection.documentReferences = {
                        retainedCount: selected.documentReferences.length,
                        omittedCount: Math.max(0, collection.totalCount - selected.documentReferences.length)
                    };
                } else {
                    const key = kind === "user_decisions" ? "userDecisions" : "work";
                    selected.remainingWork[key] = (selected.remainingWork[key] || []).concat(collection.rows);
                    const countKey = kind === "user_decisions" ? "userDecisionCount" : "workCount";
                    const omittedKey = kind === "user_decisions"
                        ? "userDecisionOmittedCount" : "workOmittedCount";
                    selected.remainingWork[countKey] = collection.totalCount;
                    selected.remainingWork[omittedKey] = Math.max(0,
                        collection.totalCount - selected.remainingWork[key].length);
                }
                render();
            })
            .catch((error) => {
                if (state.active && ticket === state.collectionTickets[kind])
                    ctx.status((error && error.message) || String(error), true);
            });
    };
    function open(project, item, presentation, machine, routeError, locatorBacked = false) {
        const previousProject = state.projectId, previousMachine = state.machine;
        state.active = true;
        if (state.projectId !== project) state.projectPresentation = null;
        if (state.projectId !== (project || null) || state.itemId !== (item || null)) {
            state.revision = -1;
            ++state.reportTicket;
            state.collectionTickets = {};
            state.reportSelection = null;
        }
        if (presentation) state.projectPresentation = presentation;
        else if (!state.projectPresentation)
            state.projectPresentation = state.projects.find((row) => row.id === project) || null;
        state.selectionLoaded = false;
        state.readStatus = "loading";
        state.projectId = project || null;
        state.machine = machine || (presentation ? presentation.machine || null
            : project && project === previousProject ? previousMachine : null);
        state.locatorBacked = locatorBacked;
        state.routeError = routeError || null;
        state.itemId = item || null;
        state.item = null;
        state.view = item ? "detail" : project ? "items" : "projects";
        elements["board-search"].value = "";
        render();
        if (elements.board) elements.board.scrollTop = 0;
        if (state.routeError) {
            state.readStatus = "error";
            ctx.status(words(ctx,
                "This Board link is invalid; no project or item was opened. ",
                "這個看板連結無效；未開啟任何專案或項目。") + state.routeError,
                true, false);
            return Promise.resolve({ error: "board_link_invalid" });
        }
        opened = load();
        const request = opened;
        request.finally(() => {
            if (opened === request) opened = null;
        });
        return request;
    }
    function openLocator(locator, error) {
        if (!locator)
            return open(null, null, null, null, error || "board_link_invalid");
        return open(locator.project, locator.item, null, locator.machine, null, true);
    }
    function select(project, item) {
        const machine = state.machine, locatorBacked = state.locatorBacked;
        const request = open(project, item, null, machine, null, locatorBacked);
        if (locatorBacked && environment.replaceURL) {
            const url = boardShareURL(machine, project, item);
            if (url) environment.replaceURL(url);
        }
        return request;
    }
    ctx.openItem = (item) => select(state.projectId, item);
    ctx.openProjectItem = (project, item) => select(project, item);
    function escape() {
        if (state.itemId) return select(state.projectId, null);
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
        ++state.reportTicket;
        state.collectionTickets = {};
        stopTimer();
        inflight = null;
        opened = null;
    }
    function enter(project, item) {
        if (opened) return opened;
        return arguments.length ? open(project, item) : open(state.projectId, state.itemId);
    }
    return { enter, leave, refresh, escape, open, openLocator, state };
}
