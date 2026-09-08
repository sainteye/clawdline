// Browser-only preview data. No mock writes reach the live board or claim Git proof.
export function createBoardMock() {
    var now = Math.floor(Date.now() / 1000), revision = 1, enabled = true, narrativeConsent = null;
    function icon(accent) { return { accent: accent, cells: [
        [null, accent, accent, accent, accent, accent, accent, null],
        [accent, accent, "#141416", accent, accent, "#141416", accent, accent],
        [accent, accent, accent, accent, accent, accent, accent, accent],
        [null, accent, accent, null, null, accent, accent, null]
    ] }; }
    var projects = [{ id: "preview-project", name: "clawdline", displayPath: "/Projects/clawdline", isStartPoint: true, icon: icon("#d97757") },
        { id: "preview-atrium", name: "atrium", displayPath: "/Projects/atrium", isStartPoint: true, icon: icon("#83c2b6") }];
    var items = [{ id: "preview-item", key: "WORK-1", projectId: "preview-project",
        title: "讓每個專案的進度一目了然", type: "feature", state: "execution", progress: { state: "review_testing", active: true, historical: false }, summary: "把對話、執行與成果串起來，手機上也能看懂正在發生什麼。",
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
    items.push({ ...structuredClone(items[0]), id: "preview-delivered", key: "WORK-2", title: "改善對話斷線後的恢復", type: "bug",
        summary: "斷線時保留閱讀位置，重新連線後接回同一段對話。", progress: { state: "delivered", active: false, historical: true },
        obligations: [], checklist: [], updatedAt: now - 7200 });
    items.push({ ...structuredClone(items[0]), id: "preview-landed", key: "WORK-3", title: "手機即時畫面自動換行", state: "closed",
        summary: "長行不再超出螢幕，閱讀程式碼與執行結果不必左右捲動。", progress: { state: "landed", active: false, historical: true },
        obligations: [], checklist: [], updatedAt: now - 86400,
        completionReport: { status: "current", version: 2, authoredAt: now - 86000, actor: "preview-root", authorship: "assistant", model: "preview-model",
            objective: "讓手機上的長內容保持可讀。", deliveredOutcomes: "長行會在可視寬度內換行，程式碼和執行結果不再要求左右捲動。",
            verificationLanding: "這是預覽敘述；下方模擬收據才是狀態依據。", remainingWork: "真實專案仍需自己的驗證與落地紀錄。",
            lessons: "閱讀介面要保留內容，也要把敘述與證據分開。",
            sourceReferences: [{ kind: "evidence", targetId: "preview-proof", label: "預覽落地紀錄", resolution: "same_item", relationship: "same_item_at_authorship", resolvedAt: now - 86000, authority: "narrative_only" }] },
        completionReportHistory: [{ id: "preview-report-v1", version: 1, status: "superseded",
            authoredAt: now - 172000, actor: "preview-root", authorship: "assistant", sourceCount: 1 }],
        landings: [{ id: "preview-proof", subject: "preview-commit", sourceId: "preview-only", summary: "僅供預覽：模擬已有落地紀錄，不代表真實 Git 驗證。" }] });
    items.push({ ...structuredClone(items[0]), id: "preview-canceled", key: "WORK-4", title: "加入手動狀態選單", state: "canceled",
        summary: "改由系統依執行證據自動更新，不再需要手動選單。", progress: { state: "canceled", active: false, historical: true }, obligations: [], checklist: [], updatedAt: now - 172800 });
    items.push({ ...structuredClone(items[0]), id: "preview-atrium-item", projectId: "preview-atrium", title: "建立新的網站體驗", type: "epic", progress: { state: "planning", active: true, historical: false } });
    items.push({ ...structuredClone(items[0]), id: "preview-coordination", key: "WORK-5", title: "整合前端與自動進度的協調", type: "coordination",
        summary: "整理兩條工作線的交界，確認同一套證據能被手機與桌面讀懂。", progress: { lifecycleApplicable: false, state: "execution", active: true },
        typeDetails: { outcomes: "對齊前端與後端的狀態判讀規則，保留已落地的歷史。", difficulties: "早期的流程紀錄不完整，不能把缺漏當成尚未完成。", improvements: "以明確的工作輪次和落地紀錄進行交接。" },
        checklist: [], milestones: [], obligations: [], artifacts: [],
        spans: [{ id: "coord-span", sessionId: "preview-session", phase: "output", startedAt: now - 3600, endedAt: null }],
        links: [{ kind: "coordinates", targetId: "preview-item", label: "讓每個專案的進度一目了然" }] });
    var receipts = new Map();
    var reportBodies = new Map([["preview-report-v1", {
        id: "preview-report-v1", version: 1, status: "superseded", authoredAt: now - 172000,
        actor: "preview-root", authorship: "assistant", objective: "較早版本的手機閱讀目標。",
        deliveredOutcomes: "保留舊版報告正文，只在讀者展開時載入。",
        verificationLanding: "預覽敘述不取代驗證。", remainingWork: "由新版報告接續。",
        lessons: "歷史版本需要可讀，但不該灌入每次 Project 讀取。",
        sourceReferences: [{ kind: "evidence", targetId: "preview-proof", label: "較早預覽紀錄",
            resolution: "same_item", relationship: "same_item_at_authorship",
            resolvedAt: now - 172000, authority: "narrative_only" }]
    }]]);
    function progressState(item) { return item.progress && item.progress.state || item.state; }
    function updateProjects() {
        projects.forEach(function (project) {
            var rows = items.filter(function (item) { return item.projectId === project.id; });
            var delivery = rows.filter(function (item) { return item.type !== "coordination"; });
            project.itemCount = rows.length;
            project.summary = {
                open: delivery.filter(function (item) { return !["landed", "canceled"].includes(progressState(item)); }).length,
                needsClarity: delivery.filter(function (item) { return ["blocked", "delivered", "unknown"].includes(progressState(item)); }).length,
                landed: delivery.filter(function (item) { return progressState(item) === "landed"; }).length,
                coordination: rows.length - delivery.length
            };
            project.summaryCoverage = "complete";
        });
    }
    updateProjects();
    function read(project, item) {
        var report = null;
        if (item && item.startsWith("report:")) {
            var parts = item.split(":");
            if (parts.length === 3) {
                item = parts[1];
                report = reportBodies.get(parts[2]) || null;
            }
        }
        return { board: { schemaVersion: 1, revision: revision, enabled: enabled,
            mode: enabled ? "board" : "standard", entitlement: { state: "free_preview", label: "Currently free" },
            narrativeConsent: narrativeConsent,
            viewer: { id: "preview", canWrite: true, canManage: true, narrativeProvider: "codex" },
            projects: projects, items: project && !item ? items.filter(function (row) { return row.projectId === project; }) : [],
            item: items.find(function (row) { return row.id === item; }) || null,
            reportSelection: report,
            readState: { status: "ready" },
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
            if (!enabled && !["set_enabled", "set_ai_consent"].includes(body.operation)) fail("board_disabled", "Preview board is read-only.");
            var item = items.find(function (row) { return row.id === body.itemId; });
            if (body.operation === "set_enabled") enabled = body.enabled;
            else if (body.operation === "set_ai_consent") {
                if (typeof body.enabled !== "boolean" || !["codex", "claude"].includes(body.provider)
                    || body.policy !== "board-reading-v1") fail("invalid_command", "Preview consent requires a named provider and policy.");
                narrativeConsent = body.enabled ? body.provider : null;
            }
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
            updateProjects();
            var result = structuredClone(read(null, item && item.id));
            if (item) result.itemId = item.id;
            receipts.set(body.requestId, { hash: hash, result: result });
            return result;
        }); }
    };
}
