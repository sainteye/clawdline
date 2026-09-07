import { T } from "../core/i18n.js";

/* Project Board is deliberately transport-free. Local and Cloud provide the same read/command
   pair through `environment`; this module owns only the DOM and the revision-aware interaction. */

var TYPES = ["feature", "refactor", "task", "bug", "coordination", "epic"];
var STATES = ["backlog", "planning", "ready", "execution", "verified", "integrated", "closed", "canceled"];
var CHECK_STATES = ["todo", "doing", "passed", "failed", "not_applicable"];
var PHASES = ["planning", "output", "review_testing", "correction", "integration"];
var sequence = 0;

var ZH = {
    projects: "Projects", items: "工作項目", detail: "項目詳情", all: "全部",
    search: "搜尋標題、編號、摘要或負責人", list: "列表", board: "看板",
    newItem: "新增項目", create: "建立", cancel: "取消", save: "儲存", edit: "編輯",
    back: "返回", loading: "讀取中…", emptyProjects: "尚無可用的 Project。",
    emptyItems: "沒有符合條件的工作項目。", readonly: "看板已關閉：目前是一般模式；歷史仍可唯讀查看。",
    free: "目前免費", truncated: "這是有限掃描的結果，較舊項目可能未列出。",
    retry: "重試", conflict: "資料已在別處更新；已重新讀取。請檢查後重試。",
    unknownError: "操作未完成，沒有任何變更被假裝成已儲存。", busy: "服務忙碌，操作尚未儲存。",
    unsupported: "目前連線不支援這項操作。", evidence: "缺少提升狀態所需的證據。",
    title: "標題", summary: "摘要", owner: "負責人", type: "類型", state: "狀態",
    parent: "上層項目 ID", next: "下一步", required: "必要", status: "狀態",
    checklist: "Checklist", milestones: "Milestones", artifacts: "成果", links: "關聯",
    obligations: "未完責任", spans: "執行紀錄", history: "歷史", evidenceTitle: "成果與驗證",
    usage: "花費", usageUnknown: "用量未知（不是 0）", usageAbsent: "沒有用量紀錄（不是 0）", measured: "已量測",
    total: "總計", output: "輸出", undeclared: "未宣告用途", partial: "資料不完整",
    acceptArtifact: "接受成果", acceptedArtifact: "成果已接受",
    add: "加入", resolve: "解除", resolved: "已解除", blocking: "阻塞中", optional: "非阻塞",
    handoff: "提出交接", handoffOwner: "建議接手者", note: "說明", session: "Session ID",
    phase: "用途階段", started: "開始", active: "進行中", openSession: "開啟 Session",
    transition: "更新生命週期", artifactTitle: "成果名稱", url: "HTTPS 網址", kind: "種類",
    target: "目標 ID", label: "顯示名稱", update: "更新", noSummary: "尚無摘要",
    noOwner: "尚未指派", noRecords: "尚無紀錄", pendingHandoff: "交接是待接手提議，不會直接改變負責人。"
};

var EN = {
    projects: "Projects", items: "Work items", detail: "Item detail", all: "All",
    search: "Search title, key, summary, or owner", list: "List", board: "Board",
    newItem: "New item", create: "Create", cancel: "Cancel", save: "Save", edit: "Edit",
    back: "Back", loading: "Loading…", emptyProjects: "No Projects are available.",
    emptyItems: "No work items match these filters.", readonly: "Board is off: standard mode is active; board history remains read-only.",
    free: "Currently free", truncated: "This is a bounded result; older items may not be listed.",
    retry: "Retry", conflict: "This data changed elsewhere. The latest revision is loaded; review and retry.",
    unknownError: "The action did not finish. Nothing has been presented as saved.", busy: "The service is busy; the action is not saved.",
    unsupported: "This connection does not support that action.", evidence: "Evidence required for this state is missing.",
    title: "Title", summary: "Summary", owner: "Owner", type: "Type", state: "State",
    parent: "Parent item ID", next: "Next", required: "Required", status: "Status",
    checklist: "Checklist", milestones: "Milestones", artifacts: "Artifacts", links: "Links",
    obligations: "Obligations", spans: "Execution", history: "History", evidenceTitle: "Evidence and verification",
    usage: "Usage", usageUnknown: "Usage unknown (not zero)", usageAbsent: "No usage record (not zero)", measured: "Measured",
    total: "Total", output: "Output", undeclared: "Undeclared phase", partial: "Partial data",
    acceptArtifact: "Accept artifact", acceptedArtifact: "Artifact accepted",
    add: "Add", resolve: "Resolve", resolved: "Resolved", blocking: "Blocking", optional: "Non-blocking",
    handoff: "Propose handoff", handoffOwner: "Proposed receiver", note: "Note", session: "Session ID",
    phase: "Phase", started: "Started", active: "Active", openSession: "Open session",
    transition: "Update lifecycle", artifactTitle: "Artifact name", url: "HTTPS URL", kind: "Kind",
    target: "Target ID", label: "Label", update: "Update", noSummary: "No summary yet",
    noOwner: "Unassigned", noRecords: "No records", pendingHandoff: "A handoff is a pending proposal; it does not immediately change the owner."
};

function words(document) {
    var lang = document && document.documentElement && document.documentElement.lang;
    /* `T` is intentionally the only imported module. Its current values tell us whether the
       central string table has been localized even when a stand-in document carries no lang. */
    var centralLooksChinese = /[\u3400-\u9fff]/.test(String(T.webBack || ""));
    return /^zh\b/i.test(String(lang || "")) || (!lang && centralLooksChinese) ? ZH : EN;
}

function clear(node) {
    while (node && node.firstChild) node.removeChild(node.firstChild);
}

function text(document, parent, tag, value, className) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    node.textContent = value == null ? "" : String(value);
    parent.appendChild(node);
    return node;
}

function button(document, parent, label, action, className) {
    var node = text(document, parent, "button", label, className || "board-button");
    node.type = "button";
    node.dataset.boardAction = action;
    return node;
}

function option(document, value, label) {
    var node = document.createElement("option");
    node.value = value;
    node.textContent = label;
    return node;
}

function fillSelect(document, node, entries, current) {
    if (!node) return;
    clear(node);
    entries.forEach(function (entry) {
        node.appendChild(option(document, typeof entry === "string" ? entry : entry.value,
                                typeof entry === "string" ? entry : entry.label));
    });
    node.value = current;
}

function field(document, form, labelText, name, value, kind, required) {
    var label = document.createElement("label");
    label.className = "board-field";
    text(document, label, "span", labelText, "board-field-label");
    var input = document.createElement(kind === "textarea" ? "textarea" : kind === "select" ? "select" : "input");
    input.name = name;
    if (kind !== "textarea" && kind !== "select") input.type = kind || "text";
    if (required) input.required = true;
    if (value != null) input.value = String(value);
    label.appendChild(input);
    form.appendChild(label);
    return input;
}

function selectField(document, form, labelText, name, entries, current) {
    var input = field(document, form, labelText, name, null, "select");
    fillSelect(document, input, entries, current);
    return input;
}

function value(form, name) {
    var stack = [form];
    while (stack.length) {
        var node = stack.shift();
        if (node.name === name) return typeof node.value === "string" ? node.value.trim() : node.value;
        stack = stack.concat(Array.from(node.children || []));
    }
    return "";
}

function checked(form, name) {
    var stack = [form];
    while (stack.length) {
        var node = stack.shift();
        if (node.name === name) return !!node.checked;
        stack = stack.concat(Array.from(node.children || []));
    }
    return false;
}

function requestId() {
    sequence += 1;
    if (globalThis.crypto && typeof globalThis.crypto.randomUUID === "function") {
        return globalThis.crypto.randomUUID();
    }
    return "board-" + Date.now().toString(36) + "-" + sequence.toString(36);
}

function asBoard(envelope) {
    if (!envelope || !envelope.board || envelope.board.schemaVersion !== 1) {
        throw Object.assign(new Error("Unsupported board response"), { code: "unsupported_capability" });
    }
    return envelope.board;
}

function errorCode(error) {
    return error && (error.code || (error.error && error.error.code)) || "board_error";
}

function errorMessage(error, copy) {
    var code = errorCode(error);
    if (code === "revision_conflict" || code === "snapshot_stale") return copy.conflict;
    if (code === "busy") return copy.busy;
    if (code === "unsupported_capability") return copy.unsupported;
    if (code === "evidence_required") return copy.evidence;
    return error && (error.message || (error.error && error.error.message)) || copy.unknownError;
}

function stateName(state, copy) {
    var zh = {
        backlog: "待規劃", planning: "規劃中", ready: "待執行", execution: "執行中",
        verified: "已驗證", integrated: "已整合", closed: "已關閉", canceled: "已取消"
    };
    return copy === ZH && zh[state] ? zh[state] : String(state || "unknown").replaceAll("_", " ");
}

function typeName(type, copy) {
    var zh = { feature: "功能", refactor: "重構", task: "任務", bug: "問題修復", coordination: "協調", epic: "大型建置" };
    return copy === ZH && zh[type] ? zh[type] : String(type || "unknown");
}

function phaseName(phase, copy) {
    var zh = { planning: "規劃", output: "主要產出", review_testing: "審查與測試", correction: "修正", integration: "整合交付" };
    return copy === ZH && zh[phase] ? zh[phase] : String(phase || "undeclared").replaceAll("_", " ");
}

function dateText(value) {
    if (value === null || value === undefined || value === "") return "—";
    var date = new Date(typeof value === "number" ? value * 1000 : value);
    return isNaN(date.getTime()) ? String(value) : date.toLocaleString();
}

function safeHTTP(value) {
    try {
        var url = new URL(value);
        return url.protocol === "https:" || url.protocol === "http:" ? url.href : null;
    } catch (_) { return null; }
}

function section(document, parent, titleValue) {
    var node = document.createElement("section");
    node.className = "board-section";
    text(document, node, "h2", titleValue, "board-section-title");
    parent.appendChild(node);
    return node;
}

function submitButton(document, form, label) {
    var row = document.createElement("div");
    row.className = "board-form-actions";
    var submit = text(document, row, "button", label, "board-button board-button-primary");
    submit.type = "submit";
    form.appendChild(row);
    return submit;
}

function inlineForm(context, parent, name, submitLabel, build, commandBody) {
    var form = context.document.createElement("form");
    form.className = "board-inline-form";
    form.dataset.boardForm = name;
    build(form);
    submitButton(context.document, form, submitLabel);
    form.addEventListener("submit", function (event) {
        event.preventDefault();
        if (!canWrite(context)) return;
        var body = commandBody(form);
        if (body) context.runCommand(body);
    });
    parent.appendChild(form);
    return form;
}

function canWrite(context) {
    return context.state.enabled && context.state.viewer && context.state.viewer.canWrite === true;
}

// Conversation identity remains durable; a terminal is only a currently observed destination.
export function resolveBoardSession(sessions, conversationID) {
    var matches = (sessions || []).filter(function (row) { return row.sessionId === conversationID; });
    return matches.length === 1 ? { id: matches[0].id } :
        { error: matches.length ? "session_ambiguous" : "session_unavailable" };
}

function itemSearch(item) {
    return [item.key, item.title, item.summary, item.owner, item.type, item.state]
        .map(function (entry) { return String(entry || "").toLocaleLowerCase(); }).join("\n");
}

function filteredItems(context) {
    var state = context.state;
    var query = String(context.elements["board-search"].value || "").trim().toLocaleLowerCase();
    var type = context.elements["board-type"].value || "all";
    var lifecycle = context.elements["board-state"].value || "all";
    return state.items.filter(function (item) {
        return (!query || itemSearch(item).includes(query))
            && (type === "all" || item.type === type)
            && (lifecycle === "all" || item.state === lifecycle);
    });
}

function checklistProgress(item) {
    var rows = item.checklist || [];
    var required = rows.filter(function (row) { return row.required; });
    var done = required.filter(function (row) { return row.status === "passed" || row.status === "not_applicable"; });
    return required.length ? done.length + "/" + required.length : "";
}

function usageText(item, copy) {
    var usage = item.usage;
    if (!usage) return copy.usageUnknown;
    if (usage.state === "absent") return copy.usageAbsent;
    var total = typeof usage.total === "number" ? usage.total
        : usage.tokens && typeof usage.tokens.total === "number" ? usage.tokens.total : null;
    var measured = typeof usage.measured === "number" ? usage.measured
        : usage.tokens && typeof usage.tokens.measured === "number" ? usage.tokens.measured : null;
    if (usage.state === "qualified" || (usage.coverageReasons || []).length) {
        return copy.measured + " " + (measured === null ? "—" : new Intl.NumberFormat().format(measured))
            + " tokens · " + (copy === ZH ? "來源或歸屬有警告" : "Qualified observation");
    }
    if (usage.state === "present" && total !== null) {
        return copy.total + " " + new Intl.NumberFormat().format(total) + " tokens";
    }
    if (measured !== null) return copy.measured + " ≥" + new Intl.NumberFormat().format(measured) + " tokens · " + copy.partial;
    return copy.usageUnknown;
}

function card(context, item) {
    var doc = context.document, copy = context.copy;
    var node = doc.createElement("button");
    node.type = "button";
    node.className = "board-item-card";
    node.dataset.boardAction = "item";
    node.dataset.boardItemId = item.id;
    node.dataset.boardType = item.type;
    node.dataset.boardState = item.state;
    var top = doc.createElement("span");
    top.className = "board-card-top";
    text(doc, top, "span", item.key || String(item.id).slice(0, 8), "board-card-key");
    text(doc, top, "span", typeName(item.type, copy), "board-type board-type-" + item.type);
    node.appendChild(top);
    text(doc, node, "strong", item.title || item.key || item.id, "board-card-title");
    text(doc, node, "span", stateName(item.state, copy), "board-card-state board-state-" + item.state);
    if (item.summary) text(doc, node, "span", item.summary, "board-card-summary");
    var blockers = (item.obligations || []).filter(function (row) { return row.blocking && !row.resolved; });
    if (blockers.length) text(doc, node, "span", copy.next + "：" + blockers[0].title, "board-card-next");
    var facts = doc.createElement("span");
    facts.className = "board-card-facts";
    text(doc, facts, "span", copy.owner + "：" + (item.owner || copy.noOwner));
    var progress = checklistProgress(item);
    if (progress) text(doc, facts, "span", progress + " " + copy.checklist);
    text(doc, facts, "span", usageText(item, copy), "board-card-usage");
    node.appendChild(facts);
    node.addEventListener("click", function () { context.openItem(item.id); });
    return node;
}

function renderProjectOptions(context) {
    var doc = context.document, target = context.elements["board-projects"];
    clear(target);
    target.hidden = false;
    target.appendChild(option(doc, "", context.copy.projects));
    if (!context.state.projects.length) {
        target.disabled = true;
        return;
    }
    target.disabled = false;
    context.state.projects.forEach(function (project) {
        var label = project.name || project.id;
        if (project.itemCount != null) label += " · " + project.itemCount;
        target.appendChild(option(doc, project.id, label));
    });
    target.value = context.state.projectId || "";
}

function renderProjects(context) {
    renderProjectOptions(context);
    context.elements["board-items"].hidden = true;
    context.elements["board-detail"].hidden = true;
}

function renderItemList(context) {
    var doc = context.document, target = context.elements["board-items"];
    clear(target);
    context.elements["board-projects"].hidden = false;
    target.hidden = false;
    context.elements["board-detail"].hidden = true;
    var items = filteredItems(context);
    if (!items.length) {
        text(doc, target, "p", context.copy.emptyItems, "board-empty");
        return;
    }
    var layout = context.state.layout;
    target.className = "board-items board-items-" + layout;
    if (layout === "board") {
        var groups = [
            { id: "planning", label: context.copy === ZH ? "規劃" : "Planning", states: ["backlog", "planning", "ready"] },
            { id: "execution", label: stateName("execution", context.copy), states: ["execution"] },
            { id: "verified", label: stateName("verified", context.copy), states: ["verified"] },
            { id: "done", label: context.copy === ZH ? "完成" : "Done", states: ["integrated", "closed", "canceled"] }
        ];
        groups.forEach(function (group) {
            var column = doc.createElement("section");
            column.className = "board-column";
            column.dataset.boardColumn = group.id;
            var rows = items.filter(function (item) { return group.states.includes(item.state); });
            text(doc, column, "h2", group.label + " · " + rows.length, "board-column-title");
            rows.forEach(function (item) { column.appendChild(card(context, item)); });
            target.appendChild(column);
        });
    } else {
        items.forEach(function (item) { target.appendChild(card(context, item)); });
    }
}

function renderStatus(context, message, error, retry) {
    var target = context.elements["board-status"];
    clear(target);
    target.className = "board-status" + (error ? " board-status-error" : "");
    if (message) text(context.document, target, "span", message);
    if (retry) {
        var again = button(context.document, target, context.copy.retry, "retry", "board-button board-retry");
        again.addEventListener("click", retry);
    }
}

function readOnlyStatus(context) {
    var messages = [];
    if (!context.state.projects.length) messages.push(context.copy.emptyProjects);
    if (!context.state.enabled) messages.push(context.copy.readonly);
    else if (!canWrite(context)) messages.push(context.copy === ZH ? "此連線僅能閱讀看板。" : "This connection may only read the board.");
    if (context.state.entitlement && context.state.entitlement.label) {
        messages.push(context.copy.free);
    }
    if (context.state.truncated) messages.push(context.copy.truncated);
    var ingestion = context.state.source && context.state.source.ingestion;
    if (ingestion && ingestion.issueCount > 0) {
        messages.push((context.copy === ZH ? "來源同步不完整" : "Source synchronization incomplete") +
            " · " + (ingestion.reasons || []).join(", "));
    }
    return messages.join(" · ");
}

function createForm(context) {
    var target = context.elements["board-items"];
    var old = [];
    var stack = [target];
    while (stack.length) {
        var node = stack.shift();
        if (node.dataset && node.dataset.boardForm === "create") old.push(node);
        stack = stack.concat(Array.from(node.children || []));
    }
    old.forEach(function (node) { if (node.parentNode) node.parentNode.removeChild(node); });
    var panel = context.document.createElement("section");
    panel.className = "board-create-panel";
    text(context.document, panel, "h2", context.copy.newItem);
    inlineForm(context, panel, "create", context.copy.create, function (form) {
        field(context.document, form, context.copy.title, "title", "", "text", true);
        selectField(context.document, form, context.copy.type, "type", TYPES, "task");
        field(context.document, form, context.copy.summary, "summary", "", "textarea");
        field(context.document, form, context.copy.owner, "owner", "", "text");
        field(context.document, form, context.copy.parent, "parentId", "", "text");
    }, function (form) {
        var titleValue = value(form, "title");
        if (!titleValue) return null;
        var body = { operation: "create", projectId: context.state.projectId,
                     title: titleValue, type: value(form, "type") || "task" };
        ["summary", "owner", "parentId"].forEach(function (key) {
            var fieldValue = value(form, key); if (fieldValue) body[key] = fieldValue;
        });
        return body;
    });
    target.appendChild(panel);
    var form = Array.from(panel.children || [])[1];
    var label = form && Array.from(form.children || [])[0];
    var first = label && Array.from(label.children || [])[1];
    if (first && first.focus) first.focus();
}

function fact(context, parent, label, valueValue) {
    var row = context.document.createElement("div");
    row.className = "board-fact";
    text(context.document, row, "span", label, "board-fact-label");
    text(context.document, row, "span", valueValue == null || valueValue === "" ? "—" : valueValue, "board-fact-value");
    parent.appendChild(row);
    return row;
}

function renderEvidenceObject(context, parent, label, rows, currentID) {
    (rows || []).forEach(function (row) {
        var line = section(context.document, parent, label);
        var current = row.id === currentID;
        var badge = current ? (context.copy === ZH ? "目前有效" : "Current")
            : (context.copy === ZH ? "歷史紀錄（不代表目前有效）" : "Historical / not current");
        if (row.kind === "finding" || label === "Findings") {
            badge = row.resolved ? (context.copy === ZH ? "已解決" : "Resolved")
                : (row.blocking ? context.copy.blocking : context.copy.optional);
        }
        text(context.document, line, "strong", (row.summary || row.id) + " · " + badge);
        fact(context, line, "Subject", row.subject);
        fact(context, line, context.copy.status, row.status);
        fact(context, line, "Source", (row.source || "unknown") + " · " + (row.actor || "unknown"));
        if (row.sourceId) fact(context, line, "Receipt", row.sourceId);
        if (row.at) fact(context, line, context.copy.started, dateText(row.at));
    });
}

function renderDetail(context) {
    var doc = context.document, target = context.elements["board-detail"], item = context.state.item;
    clear(target);
    context.elements["board-projects"].hidden = false;
    context.elements["board-items"].hidden = true;
    target.hidden = false;
    if (!item) {
        text(doc, target, "p", context.copy.noRecords, "board-empty");
        return;
    }
    var header = doc.createElement("header");
    header.className = "board-detail-header";
    text(doc, header, "span", (item.key || item.id) + " · " + typeName(item.type, context.copy), "board-detail-key");
    text(doc, header, "h1", item.title || item.key || item.id, "board-detail-title");
    text(doc, header, "p", item.summary || context.copy.noSummary, "board-detail-summary");
    var chips = doc.createElement("div");
    chips.className = "board-detail-chips";
    text(doc, chips, "span", stateName(item.state, context.copy), "board-chip board-state-" + item.state);
    text(doc, chips, "span", context.copy.owner + "：" + (item.owner || context.copy.noOwner), "board-chip");
    header.appendChild(chips);
    if (canWrite(context)) {
        var edit = button(doc, header, context.copy.edit, "edit", "board-button");
        edit.addEventListener("click", function () { showEdit(context, header, item); });
    }
    target.appendChild(header);

    Object.keys(item.projection || {}).forEach(function (name) {
        var coverage = item.projection[name];
        if (!coverage || !(coverage.omittedCount > 0)) return;
        text(doc, target, "p", name + " · " + coverage.omittedCount +
            (context.copy === ZH ? " 筆未在此載入（紀錄仍保留）" : " records omitted from this view (retained in storage)"), "board-detail-summary");
    });
    var omittedHistory = item.projection && item.projection.history && item.projection.history.omittedCount || 0;
    var evictedHistory = Math.max(0, (item.historyDroppedCount || 0) - omittedHistory);
    if (evictedHistory) text(doc, target, "p", evictedHistory +
        (context.copy === ZH ? " 筆較早歷史已超過保留上限，不再保存。" : " earlier history records are no longer retained."), "board-detail-summary");

    var usage = section(doc, target, context.copy.usage);
    text(doc, usage, "p", usageText(item, context.copy), item.usage ? "board-usage-value" : "board-usage-unknown");
    if (item.usage) {
        var usageData = item.usage;
        if (usageData.state === "partial" || usageData.incompleteRows) {
            fact(context, usage, context.copy.status, context.copy.partial + (usageData.incompleteRows ? " · " + usageData.incompleteRows : ""));
        }
        if (Array.isArray(usageData.coverageReasons) && usageData.coverageReasons.length) {
            fact(context, usage, context.copy.status, usageData.coverageReasons.join(" · "));
        }
        if (usageData.state === "present" && typeof usageData.output === "number") {
            fact(context, usage, context.copy.output, new Intl.NumberFormat().format(usageData.output) + " tokens");
        }
        Object.keys(usageData.parts || {}).forEach(function (key) {
            if (typeof usageData.parts[key] === "number") {
                fact(context, usage, key, new Intl.NumberFormat().format(usageData.parts[key]));
            }
        });
        Object.keys(usageData.phases || {}).forEach(function (key) {
            var reading = usageData.phases[key], amount = typeof reading === "number" ? reading
                : reading && typeof reading.total === "number" ? reading.total : null;
            if (amount !== null) fact(context, usage, phaseName(key, context.copy), new Intl.NumberFormat().format(amount) + " tokens");
        });
        if (usageData.undeclaredRows || usageData.phaseReason) {
            fact(context, usage, context.copy.undeclared,
                 (usageData.undeclaredRows ? usageData.undeclaredRows + " records" : context.copy.usageUnknown)
                 + (usageData.phaseReason ? " · " + String(usageData.phaseReason).replaceAll("_", " ") : ""));
        }
        (usageData.costSeries || []).forEach(function (series) {
            if (typeof series.value === "number") {
                fact(context, usage, "Cost", new Intl.NumberFormat(undefined, { maximumSignificantDigits: 6 }).format(series.value)
                     + " " + series.unit + " · " + series.basis);
            }
        });
        if (usageData.missingCostRows) fact(context, usage, "Cost", usageData.missingCostRows + " unknown records");
    }

    var checklist = section(doc, target, context.copy.checklist);
    (item.checklist || []).forEach(function (row) {
        var line = doc.createElement("div");
        line.className = "board-check-row";
        text(doc, line, "span", row.title, "board-check-title");
        text(doc, line, "span", row.required ? context.copy.required : "", "board-check-required");
        if (canWrite(context)) {
            var status = doc.createElement("select");
            status.setAttribute("aria-label", row.title + " " + context.copy.status);
            fillSelect(doc, status, CHECK_STATES, row.status);
            status.addEventListener("change", function () {
                context.runCommand({ operation: "checklist", itemId: item.id,
                                     checklistId: row.id, status: status.value });
            });
            line.appendChild(status);
        } else text(doc, line, "span", row.status, "board-check-status");
        checklist.appendChild(line);
    });
    if (!(item.checklist || []).length) text(doc, checklist, "p", context.copy.noRecords, "board-empty-small");
    if (canWrite(context)) inlineForm(context, checklist, "checklist", context.copy.add, function (form) {
        field(doc, form, context.copy.title, "title", "", "text", true);
        var required = field(doc, form, context.copy.required, "required", "", "checkbox"); required.checked = true;
    }, function (form) {
        var titleValue = value(form, "title");
        return titleValue ? { operation: "checklist", itemId: item.id, title: titleValue,
                              required: checked(form, "required") } : null;
    });

    var milestones = section(doc, target, context.copy.milestones);
    (item.milestones || []).forEach(function (row) {
        var line = doc.createElement("div"); line.className = "board-milestone-row";
        text(doc, line, "span", row.title, "board-milestone-title");
        if (canWrite(context)) {
            var status = doc.createElement("select");
            status.setAttribute("aria-label", row.title + " " + context.copy.status);
            fillSelect(doc, status, CHECK_STATES, row.status);
            status.addEventListener("change", function () {
                context.runCommand({ operation: "milestone", itemId: item.id,
                                     milestoneId: row.id, status: status.value });
            });
            line.appendChild(status);
        } else text(doc, line, "span", row.status);
        milestones.appendChild(line);
    });
    if (canWrite(context)) inlineForm(context, milestones, "milestone", context.copy.add, function (form) {
        field(doc, form, context.copy.title, "title", "", "text", true);
    }, function (form) {
        var titleValue = value(form, "title");
        return titleValue ? { operation: "milestone", itemId: item.id, title: titleValue } : null;
    });

    var obligations = section(doc, target, context.copy.obligations);
    (item.obligations || []).forEach(function (row) {
        var line = doc.createElement("div"); line.className = "board-obligation";
        text(doc, line, "strong", row.title);
        text(doc, line, "span", (row.owner || context.copy.noOwner) + " · "
             + (row.resolved ? context.copy.resolved : row.blocking ? context.copy.blocking : context.copy.optional));
        if (canWrite(context) && !row.resolved) {
            var resolve = button(doc, line, context.copy.resolve, "resolve-obligation", "board-button board-button-small");
            resolve.dataset.boardObligationId = row.id;
            resolve.addEventListener("click", function () {
                context.runCommand({ operation: "resolve_obligation", itemId: item.id,
                                     obligationId: row.id, note: "Resolved from Project Board" });
            });
        }
        obligations.appendChild(line);
    });
    if (canWrite(context)) inlineForm(context, obligations, "obligation", context.copy.add, function (form) {
        field(doc, form, context.copy.title, "title", "", "text", true);
        field(doc, form, context.copy.owner, "owner", "", "text", true);
        var block = field(doc, form, context.copy.blocking, "blocking", "", "checkbox"); block.checked = true;
    }, function (form) {
        var titleValue = value(form, "title"), ownerValue = value(form, "owner");
        return titleValue && ownerValue ? { operation: "obligation", itemId: item.id,
            title: titleValue, owner: ownerValue, blocking: checked(form, "blocking") } : null;
    });

    var artifacts = section(doc, target, context.copy.artifacts);
    (item.artifacts || []).forEach(function (row) {
        var url = safeHTTP(row.url), line;
        if (url) {
            line = text(doc, artifacts, "a", row.title || row.url, "board-artifact-link");
            line.href = url; line.target = "_blank"; line.rel = "noopener noreferrer";
        } else {
            line = text(doc, artifacts, "div", row.title || row.url, "board-artifact-invalid");
        }
        line.dataset.boardArtifactKind = row.kind || "other";
        if (item.type === "task") {
            var acceptance = (item.artifactAcceptances || []).find(function (proof) {
                return proof.id === (item.currentEvidence || {}).artifactAcceptanceId && proof.artifactId === row.id;
            });
            if (acceptance) {
                text(doc, artifacts, "span", context.copy.acceptedArtifact, "board-artifact-accepted");
            } else if (canWrite(context) && context.state.viewer.canManage === true) {
                var accept = button(doc, artifacts, context.copy.acceptArtifact, "accept-artifact", "board-button board-button-small");
                accept.dataset.boardArtifactId = row.id;
                accept.addEventListener("click", function () {
                    context.runCommand({ operation: "accept_artifact", itemId: item.id, artifactId: row.id });
                });
            }
        }
    });
    if (canWrite(context)) inlineForm(context, artifacts, "artifact", context.copy.add, function (form) {
        field(doc, form, context.copy.artifactTitle, "title", "", "text", true);
        field(doc, form, context.copy.url, "url", "", "url", true);
        selectField(doc, form, context.copy.kind, "kind", ["document", "website", "deployment", "commit", "other"], "document");
    }, function (form) {
        var titleValue = value(form, "title"), url = value(form, "url");
        return titleValue && safeHTTP(url) ? { operation: "artifact", itemId: item.id, title: titleValue,
            url: url, kind: value(form, "kind") || "other" } : null;
    });

    var links = section(doc, target, context.copy.links);
    (item.links || []).forEach(function (row) {
        var line = doc.createElement("div"); line.className = "board-link-row";
        line.dataset.boardLinkKind = row.kind;
        text(doc, line, "span", (row.label || row.targetId) + " · " + row.kind, "board-link-label");
        if (row.kind === "session") {
            var open = button(doc, line, context.copy.openSession, "open-session", "board-button board-button-small");
            open.addEventListener("click", function () {
                var answer = context.openSession(row.targetId);
                if (answer && answer.error) renderStatus(context,
                    context.copy === ZH ? "這段對話目前沒有唯一可開啟的 Session（可能已關閉或連線不明）。" :
                        "This conversation has no unique live Session: it may be closed or ambiguous.", true);
            });
        }
        links.appendChild(line);
    });
    if (canWrite(context)) inlineForm(context, links, "link", context.copy.add, function (form) {
        selectField(doc, form, context.copy.kind, "kind", ["session", "task", "worktree", "related", "blocks", "coordinates"], "related");
        field(doc, form, context.copy.target, "targetId", "", "text", true);
        field(doc, form, context.copy.label, "label", "", "text", true);
    }, function (form) {
        var targetId = value(form, "targetId"), label = value(form, "label");
        return targetId && label ? { operation: "link", itemId: item.id,
            kind: value(form, "kind"), targetId: targetId, label: label } : null;
    });

    var spans = section(doc, target, context.copy.spans);
    (item.spans || []).forEach(function (row) {
        var line = doc.createElement("div"); line.className = "board-span-row";
        text(doc, line, "strong", row.sessionId);
        text(doc, line, "span", phaseName(row.phase, context.copy) + " · " + dateText(row.startedAt)
             + " – " + (row.endedAt ? dateText(row.endedAt) : context.copy.active));
        spans.appendChild(line);
    });
    if (canWrite(context)) inlineForm(context, spans, "span", context.copy.add, function (form) {
        field(doc, form, context.copy.session, "sessionId", "", "text", true);
        selectField(doc, form, context.copy.phase, "phase", PHASES, "output");
    }, function (form) {
        var sessionId = value(form, "sessionId");
        return sessionId ? { operation: "span", itemId: item.id, sessionId: sessionId,
                             phase: value(form, "phase") } : null;
    });

    var handoff = section(doc, target, context.copy.handoff);
    text(doc, handoff, "p", context.copy.pendingHandoff, "board-section-help");
    if (context.state.viewer && context.state.viewer.id) {
        text(doc, handoff, "p", (context.copy === ZH ? "目前接收身分：" : "Your receiver ID: ") +
            context.state.viewer.id, "board-section-help");
    }
    if (item.handoff) {
        text(doc, handoff, "p", item.handoff.proposedOwner + " · " + item.handoff.note, "board-section-help");
        if (canWrite(context) && context.state.viewer &&
            context.state.viewer.id === item.handoff.proposedOwner) {
            inlineForm(context, handoff, "accept_handoff", context.copy === ZH ? "接受交接" : "Accept handoff", function (form) {
                field(doc, form, context.copy.note, "note", "", "textarea", true);
            }, function (form) {
                var note = value(form, "note");
                return note ? { operation: "accept_handoff", itemId: item.id, note: note } : null;
            });
        }
    }
    if (canWrite(context)) inlineForm(context, handoff, "handoff", context.copy.handoff, function (form) {
        field(doc, form, context.copy.handoffOwner, "owner", "", "text", true);
        field(doc, form, context.copy.note, "note", "", "textarea", true);
    }, function (form) {
        var ownerValue = value(form, "owner"), note = value(form, "note");
        return ownerValue && note ? { operation: "handoff", itemId: item.id, owner: ownerValue, note: note } : null;
    });

    if (canWrite(context)) {
        var transition = section(doc, target, context.copy.transition);
        inlineForm(context, transition, "transition", context.copy.update, function (form) {
            selectField(doc, form, context.copy.state, "state", STATES, item.state);
            field(doc, form, context.copy.note, "note", "", "textarea");
        }, function (form) {
            var body = { operation: "transition", itemId: item.id, state: value(form, "state") };
            var note = value(form, "note"); if (note) body.note = note;
            return body;
        });
    }

    var evidence = section(doc, target, context.copy.evidenceTitle);
    var current = item.currentEvidence || {};
    renderEvidenceObject(context, evidence, "Findings", item.findings);
    renderEvidenceObject(context, evidence, "Verification", item.verifications, current.verificationId);
    renderEvidenceObject(context, evidence, "Landing", item.landings, current.landingId);
    renderEvidenceObject(context, evidence, "Artifact acceptance", item.artifactAcceptances, current.artifactAcceptanceId);
    renderEvidenceObject(context, evidence, "Attempt summary", item.evidenceSummaries);
    if (evidence.children.length === 1) text(doc, evidence, "p", context.copy.noRecords, "board-empty-small");

    var history = section(doc, target, context.copy.history);
    (item.history || []).forEach(function (row) {
        var line = doc.createElement("div"); line.className = "board-history-row";
        text(doc, line, "span", dateText(row.at), "board-history-at");
        text(doc, line, "strong", row.summary || row.kind);
        text(doc, line, "span", (row.actor || "unknown") + " · " + (row.kind || "event"));
        history.appendChild(line);
    });
}

function showEdit(context, header, item) {
    var existing = Array.from(header.children || []).filter(function (node) {
        return node.dataset && node.dataset.boardForm === "edit";
    });
    existing.forEach(function (node) { header.removeChild(node); });
    inlineForm(context, header, "edit", context.copy.save, function (form) {
        field(context.document, form, context.copy.title, "title", item.title, "text", true);
        field(context.document, form, context.copy.summary, "summary", item.summary || "", "textarea");
        field(context.document, form, context.copy.owner, "owner", item.owner || "", "text");
        selectField(context.document, form, context.copy.type, "type", TYPES, item.type);
    }, function (form) {
        return { operation: "update", itemId: item.id, title: value(form, "title"),
                 summary: value(form, "summary"), owner: value(form, "owner"), type: value(form, "type") };
    });
}

export function bindBoardPage(elements, environment) {
    environment = environment || {};
    var document = environment.document || globalThis.document;
    var copy = words(document);
    var state = {
        view: "projects", layout: "list", projects: [], items: [], item: null, projectId: null, itemId: null,
        revision: 0, enabled: true, mode: "board", entitlement: null, truncated: false,
        readTicket: 0, pending: null, sending: false, active: false, openPromise: null
    };
    var read = typeof environment.read === "function" ? environment.read : function () {
        return Promise.reject(Object.assign(new Error(copy.unsupported), { code: "unsupported_capability" }));
    };
    var command = typeof environment.command === "function" ? environment.command : function () {
        return Promise.reject(Object.assign(new Error(copy.unsupported), { code: "unsupported_capability" }));
    };
    var navigate = typeof environment.navigate === "function" ? environment.navigate : function () {};
    var openSession = typeof environment.openSession === "function" ? environment.openSession : function () {};

    var context = { document: document, elements: elements, copy: copy, state: state,
        navigate: navigate, openSession: openSession };
    var blockedControls = new Map();
    function updateCommandControls() {
        blockedControls.forEach(function (disabled, node) { node.disabled = disabled; });
        blockedControls.clear();
        elements["board-new"].disabled = !canWrite(context) || !state.projectId;
        if (!state.pending && !state.sending) return;
        var stack = [elements["board-items"], elements["board-detail"], elements["board-new"]];
        while (stack.length) {
            var node = stack.pop();
            if (!node) continue;
            if (["BUTTON", "INPUT", "SELECT", "TEXTAREA"].includes(node.tagName)) {
                blockedControls.set(node, !!node.disabled); node.disabled = true;
            }
            stack.push.apply(stack, Array.from(node.children || []));
        }
    }

    function setView(view) {
        state.view = view;
        if (view === "projects") renderProjects(context);
        else if (view === "items") renderItemList(context);
        else renderDetail(context);
    }

    function adopt(board, requestedView) {
        if (typeof board.revision === "number" && board.revision < state.revision) return false;
        state.revision = typeof board.revision === "number" ? board.revision : state.revision;
        state.enabled = board.enabled !== false;
        state.mode = board.mode || (state.enabled ? "board" : "standard");
        state.entitlement = board.entitlement || null;
        state.viewer = board.viewer || state.viewer || null;
        state.projects = Array.isArray(board.projects) ? board.projects : state.projects;
        state.items = Array.isArray(board.items) ? board.items : state.items;
        state.truncated = !!board.truncated;
        state.source = board.source || null;
        renderProjectOptions(context);
        if (board.item !== undefined && (board.item || requestedView === "detail")) state.item = board.item;
        elements["board-new"].disabled = !canWrite(context) || !state.projectId;
        if (typeof environment.onMode === "function") environment.onMode(board);
        if (typeof environment.onProjects === "function") environment.onProjects(state.projects.slice());
        setView(requestedView || state.view);
        if (state.pending && !state.sending) renderStatus(context, copy.conflict, true, retryPending);
        else renderStatus(context, readOnlyStatus(context));
        updateCommandControls();
        return true;
    }

    function load(projectId, itemId, requestedView) {
        var ticket = ++state.readTicket;
        renderStatus(context, copy.loading);
        return Promise.resolve().then(function () { return read(projectId, itemId); }).then(function (answer) {
            if (!state.active || ticket !== state.readTicket) return;
            adopt(asBoard(answer), requestedView);
        }).catch(function (error) {
            if (!state.active || ticket !== state.readTicket) return;
            renderStatus(context, errorMessage(error, copy), true, function () {
                load(projectId, itemId, requestedView);
            });
        });
    }

    function openProject(projectId) {
        state.projectId = projectId;
        state.itemId = null; state.item = null;
        elements["board-new"].disabled = !canWrite(context);
        navigate("board", projectId, null);
        return load(projectId, undefined, "items");
    }

    function openItem(itemId) {
        state.itemId = itemId;
        navigate("board", state.projectId, itemId);
        return load(state.projectId, itemId, "detail");
    }

    function refresh() {
        applyCopy();
        return load(state.projectId || undefined, state.itemId || undefined, state.view);
    }

    function retryPending() {
        if (!state.pending) return Promise.resolve();
        var pending = state.pending;
        var body = Object.assign({}, pending.body);
        if (pending.rebase) {
            body.requestId = requestId();
            body.expectedRevision = state.revision;
        }
        /* Once a CAS action has been rebased it is a new ordinary idempotent action. If that new
           send fails transiently, its next retry must keep this new body byte-for-byte stable. */
        return send(body, false);
    }

    function send(body, rebased) {
        if (state.sending) return Promise.resolve();
        var outgoing = Object.assign({}, body);
        outgoing.requestId = outgoing.requestId || requestId();
        outgoing.expectedRevision = typeof outgoing.expectedRevision === "number"
            ? outgoing.expectedRevision : state.revision;
        var attempt = { body: Object.assign({}, outgoing), requestId: outgoing.requestId, rebase: !!rebased };
        state.pending = attempt; state.sending = true;
        updateCommandControls();
        ++state.readTicket; // an older GET cannot overwrite a command result
        renderStatus(context, copy.loading);
        return Promise.resolve().then(function () { return command(outgoing); }).then(function (answer) {
            if (answer && answer.error) throw answer;
            var board = asBoard(answer);
            state.pending = null;
            if (answer.itemId) state.itemId = answer.itemId;
            if (board.item && state.item && board.item.id === state.item.id
                    && !Object.prototype.hasOwnProperty.call(board.item, "usage") && state.item.usage) {
                board.item = Object.assign({}, board.item, { usage: state.item.usage });
            }
            adopt(board, board.item ? "detail" : state.itemId && state.view === "detail" ? "detail" : "items");
            /* Command snapshots intentionally contain only store-owned facts. Re-read through
               the integration projection so UsageLedger enrichment is neither erased nor guessed. */
            return load(state.projectId || undefined, state.itemId || undefined, state.view)
                .then(function () { return answer; });
        }).catch(function (error) {
            var code = errorCode(error);
            if (code === "revision_conflict" || code === "snapshot_stale") {
                attempt.rebase = true; state.pending = attempt;
                return load(state.projectId || undefined, state.itemId || undefined, state.view).then(function () {
                    renderStatus(context, errorMessage(error, copy), true, retryPending);
                });
            }
            var ambiguous = code === "board_error" || /offline|busy|timeout|unavailable|network|connection|persistence_failed/.test(code);
            if (!ambiguous) state.pending = null;
            renderStatus(context, errorMessage(error, copy), true, ambiguous ? retryPending : null);
        }).finally(function () {
            state.sending = false;
            updateCommandControls();
        });
    }

    context.openProject = openProject;
    context.openItem = openItem;
    context.runCommand = function (body) {
        if (!canWrite(context) || state.sending) return Promise.resolve();
        if (state.pending) {
            renderStatus(context, copy === ZH ? "請先重試上一筆尚未確認的操作。" : "Retry the unconfirmed action before sending another.", true, retryPending);
            return Promise.resolve();
        }
        return send(body, false);
    };

    function applyCopy() {
        if (elements["board-refresh"]) elements["board-refresh"].textContent = words(document) === ZH ? "重新整理" : "Refresh";
        var selectedType = elements["board-type"].value || "all";
        var selectedState = elements["board-state"].value || "all";
        copy = words(document);
        context.copy = copy;
        fillSelect(document, elements["board-type"], [{ value: "all", label: copy.all }].concat(
            TYPES.map(function (type) { return { value: type, label: typeName(type, copy) }; })), selectedType);
        fillSelect(document, elements["board-state"], [{ value: "all", label: copy.all }].concat(
            STATES.map(function (name) { return { value: name, label: stateName(name, copy) }; })), selectedState);
        elements["board-search"].placeholder = copy.search;
        elements["board-search"].setAttribute("aria-label", copy.search);
        elements["board-layout"].textContent = copy.board + " / " + copy.list;
        elements["board-layout"].setAttribute("aria-label", copy.board + " / " + copy.list);
        elements["board-layout"].setAttribute("aria-pressed", state.layout === "board" ? "true" : "false");
        elements["board-layout"].dataset.boardLayout = state.layout;
        elements["board-type"].setAttribute("aria-label", copy.type);
        elements["board-state"].setAttribute("aria-label", copy.state);
        elements["board-new"].textContent = copy.newItem;
        elements["board-back"].textContent = copy.back;
    }
    applyCopy();
    elements["board-new"].disabled = true;

    function filter() { if (state.view === "items") renderItemList(context); }
    elements["board-projects"].addEventListener("change", function () {
        if (elements["board-projects"].value) openProject(elements["board-projects"].value);
    });
    elements["board-search"].addEventListener("input", filter);
    elements["board-layout"].addEventListener("click", function () {
        state.layout = state.layout === "list" ? "board" : "list";
        elements["board-layout"].dataset.boardLayout = state.layout;
        elements["board-layout"].setAttribute("aria-pressed", state.layout === "board" ? "true" : "false");
        filter();
    });
    elements["board-type"].addEventListener("change", filter);
    elements["board-state"].addEventListener("change", filter);
    elements["board-new"].addEventListener("click", function () {
        if (canWrite(context) && !state.pending && state.view === "items") createForm(context);
    });

    function escape() {
        if (state.view === "detail") {
            state.itemId = null; state.item = null; navigate("board", state.projectId, null); setView("items"); return;
        }
        if (state.view === "items") {
            state.projectId = null; navigate("board", null, null); elements["board-new"].disabled = true;
            setView("projects"); return;
        }
        navigate("sessions");
    }
    elements["board-back"].addEventListener("click", escape);
    if (elements["board-refresh"]) elements["board-refresh"].addEventListener("click", function () {
        if (!state.sending) refresh();
    });

    function enter(projectId, itemId) {
        if (arguments.length === 0 && state.openPromise) return state.openPromise;
        applyCopy();
        state.active = true;
        if (arguments.length || (!state.projectId && !state.itemId)) {
            state.projectId = projectId || null;
            state.itemId = itemId || null;
        }
        state.view = itemId ? "detail" : projectId ? "items" : "projects";
        if (!arguments.length && state.itemId) state.view = "detail";
        else if (!arguments.length && state.projectId) state.view = "items";
        return load(state.projectId || undefined, state.itemId || undefined, state.view);
    }

    function leave() { state.active = false; ++state.readTicket; }

    function open(projectId, itemId) {
        applyCopy();
        state.active = true;
        state.projectId = projectId || null;
        state.itemId = itemId || null;
        state.view = itemId ? "detail" : projectId ? "items" : "projects";
        var promise = load(projectId, itemId, state.view);
        state.openPromise = promise;
        promise.then(function () {
            if (state.openPromise === promise) state.openPromise = null;
        }, function () {
            if (state.openPromise === promise) state.openPromise = null;
        });
        return promise;
    }

    setView("projects");
    return { enter: enter, leave: leave, refresh: refresh, escape: escape, open: open, state: state };
}
