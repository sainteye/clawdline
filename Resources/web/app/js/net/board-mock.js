// Browser-only preview data. No mock writes reach the live board or claim Git proof.
export function createBoardMock() {
    var now = Math.floor(Date.now() / 1000), revision = 1, enabled = true;
    var projects = [{ id: "preview-project", name: "Clawdline", itemCount: 1 }];
    var items = [{ id: "preview-item", key: "WORK-1", projectId: "preview-project",
        title: "整合 Project 看板", type: "feature", state: "execution", summary: "預覽資料：派工、進度、用量與驗收集中在同一項目。",
        owner: "Clawdfather", parentId: null, createdAt: now - 3600, updatedAt: now,
        checklist: [{ id: "c1", title: "設定預設開啟", status: "passed", required: true },
            { id: "c2", title: "手機與桌面互動驗證", status: "doing", required: true }],
        milestones: [{ id: "m1", title: "首版整合", status: "doing" }],
        artifacts: [{ id: "a1", title: "設計說明", url: "https://linear.app/docs/conceptual-model", kind: "document" }],
        links: [{ id: "l1", kind: "session", targetId: "8F3A-1C", label: "整合 Session" }],
        obligations: [{ id: "o1", title: "獨立審查", owner: "reviewer", blocking: true, resolved: false }],
        history: [{ id: "h1", at: now, actor: "preview", kind: "created", summary: "建立預覽項目" }],
        spans: [], evidence: [], usage: { state: "partial", rows: 2, measured: 14500, total: null,
            output: 2400, parts: { input: 12100, output: 2400 }, incompleteRows: 1,
            phases: {}, undeclaredRows: 2, costSeries: [], missingCostRows: 2 } }];
    var receipts = new Map();
    function read(project, item) {
        projects[0].itemCount = items.length;
        return { board: { schemaVersion: 1, revision: revision, enabled: enabled,
            mode: enabled ? "board" : "standard", entitlement: { state: "free_preview", label: "Currently free" },
            viewer: { id: "preview", canWrite: true, canManage: true },
            projects: projects, items: items.filter(function (row) { return !project || row.projectId === project; }),
            item: items.find(function (row) { return row.id === item; }) || null,
            updatedAt: now, truncated: false } };
    }
    function fail(code, message) { var error = new Error(message); error.code = code; throw error; }
    return {
        board: function (project, item) { return Promise.resolve(structuredClone(read(project, item))); },
        boardCommand: function (body) { return Promise.resolve().then(function () {
            var hash = JSON.stringify(body), saved = receipts.get(body.requestId);
            if (saved) {
                if (saved.hash !== hash) fail("idempotency_conflict", "Preview request identity conflict");
                return structuredClone(saved.result);
            }
            if (body.expectedRevision !== revision) fail("revision_conflict", "Preview changed; refresh and retry.");
            if (!enabled && body.operation !== "set_enabled") fail("board_disabled", "Preview board is read-only.");
            var item = items.find(function (row) { return row.id === body.itemId; });
            if (body.operation === "set_enabled") enabled = body.enabled;
            else if (body.operation === "create") {
                item = { id: crypto.randomUUID(), key: "WORK-" + (items.length + 1), projectId: body.projectId,
                    title: body.title, type: body.type, summary: body.summary || "", state: "backlog", owner: body.owner || "",
                    parentId: body.parentId || null, createdAt: now, updatedAt: now,
                    checklist: [], milestones: [], artifacts: [], links: [], obligations: [], history: [], spans: [], evidence: [] };
                items.push(item);
            } else if (!item) fail("not_found", "Preview item not found.");
            else if (body.operation === "update") {
                ["title", "type", "summary", "owner"].forEach(function (key) { if (body[key] !== undefined) item[key] = body[key]; });
            } else if (body.operation === "transition" && ["backlog", "planning", "ready", "execution", "canceled"].includes(body.state)) {
                item.state = body.state;
            } else fail("preview_unsupported", "This preview cannot certify or modify evidence. Use the running app for this operation.");
            revision += 1;
            var result = structuredClone(read(null, item && item.id));
            if (item) result.itemId = item.id;
            receipts.set(body.requestId, { hash: hash, result: result });
            return result;
        }); }
    };
}
