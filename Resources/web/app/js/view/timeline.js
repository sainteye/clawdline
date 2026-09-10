const STATUS = {
    available: ["Available", "已上線", "success"],
    limited: ["Limited rollout", "有限上線", "warning"],
    partial: ["Partially available", "部分上線", "warning"],
    deploy_pending: ["Deployed · awaiting availability", "部署成功，待可用性確認", "pending"],
    landed_to_git: ["In Git", "已進 Git", "neutral"],
    deploy_failed: ["Deployment failed", "上線失敗", "danger"],
    rolled_back: ["Rolled back", "已回滾", "danger"],
    partial_rollback: ["Partially rolled back", "部分回滾", "warning"],
    superseded: ["Superseded", "已取代", "neutral"],
    operation_pending: ["Applied · awaiting verification", "操作完成，待驗證", "pending"],
    upcoming: ["Not deployed", "尚未上線", "neutral"],
    unknown: ["Availability unknown", "上線狀態未知", "neutral"]
};
const CATEGORIES = {
    deploy: ["🚀", "Deploy", "部署"], server: ["🖥", "Server", "伺服器"],
    architecture: ["🧱", "Server architecture", "伺服器架構"],
    feature: ["✨", "Feature", "功能"], operation: ["🔧", "Operation", "操作"]
};

function chinese(locale) { return /^zh(?:-|$)/i.test(String(locale || "")); }
function words(locale, en, zh) { return chinese(locale) ? zh : en; }

export function timelineStatus(value, locale) {
    const row = STATUS[value] || STATUS.unknown;
    return { label: chinese(locale) ? row[1] : row[0], tone: row[2] };
}

export function githubCommitURL(repositoryID, commit) {
    if (!/^github:[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(String(repositoryID || "")) ||
        !/^[0-9a-f]{7,64}$/i.test(String(commit || ""))) return null;
    return "https://github.com/" + repositoryID.slice(7) + "/commit/" + commit.toLowerCase();
}

export function timelineDateGroup(value, now = new Date(), locale = "en") {
    if (typeof value !== "number" || !Number.isFinite(value)) return words(locale, "Time unknown", "時間未知");
    const date = new Date(value * 1000), start = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const day = Math.floor((start - new Date(date.getFullYear(), date.getMonth(), date.getDate())) / 86400000);
    if (day === 0) return words(locale, "Today", "今天");
    if (day === 1) return words(locale, "Yesterday", "昨天");
    if (day > 1 && day < 7) return words(locale, "This week", "本週");
    return new Intl.DateTimeFormat(locale, { year: date.getFullYear() === now.getFullYear() ? undefined : "numeric", month: "long", day: "numeric" }).format(date);
}

function element(doc, tag, text, cls) {
    const node = doc.createElement(tag);
    if (cls) node.className = cls;
    if (text != null) node.textContent = String(text);
    return node;
}
function clear(node) { if (node) node.replaceChildren(); }
function eventTime(entry) {
    const projection = entry && entry.projection || {};
    return typeof projection.effectiveAt === "number" ? projection.effectiveAt
        : (typeof projection.observedAt === "number" ? projection.observedAt : null);
}
function clock(value, locale) {
    return typeof value === "number" ? new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit" }).format(new Date(value * 1000))
        : words(locale, "Time unknown", "時間未知");
}
function category(entry, locale) {
    const row = CATEGORIES[entry.primaryCategory] || CATEGORIES.feature;
    return { icon: row[0], label: chinese(locale) ? row[2] : row[1] };
}

export function bindTimelinePage(elements, environment) {
    const root = elements.timeline, doc = root.ownerDocument || document;
    const locale = () => doc.documentElement.lang || "en";
    const state = { projectId: null, presentation: null, entryId: null, cursor: null,
        environment: "production", category: null, includeUpcoming: false,
        revision: null, enabled: true, canManage: false, loading: false, pendingMode: null,
        entries: [] };

    function status(text) { if (elements["timeline-status"]) elements["timeline-status"].textContent = text || ""; }
    function settingsStatus(text) { if (elements["settings-timeline-status"]) elements["settings-timeline-status"].textContent = text || ""; }
    function controls() {
        const copy = {
            "timeline-back": ["Back", "返回"], "timeline-board-tab": ["Board", "看板"],
            "timeline-refresh": ["Refresh", "重新整理"],
            "timeline-environment-label": ["Environment", "環境"],
            "timeline-category-label": ["Category", "類型"],
            "timeline-upcoming-label": ["Include upcoming / Git history", "包含未上線與 Git 歷史"],
            "settings-timeline-title": ["Enable Project Timeline", "啟用專案時間軸"],
            "settings-timeline-say": ["Keep a traceable history of deliveries and availability.", "保留可追溯的交付與上線紀錄。"],
            "settings-timeline-history": ["Open timeline", "開啟時間軸"]
        };
        Object.entries(copy).forEach(([id, labels]) => {
            if (elements[id]) elements[id].textContent = words(locale(), ...labels);
        });
        function options(id, rows, selected) {
            const select = elements[id]; if (!select) return;
            clear(select);
            rows.forEach(([value, en, zh]) => {
                const option = element(doc, "option", words(locale(), en, zh));
                option.value = value; select.appendChild(option);
            });
            select.value = selected;
        }
        options("timeline-environment", [
            ["production", "Production", "正式環境"], ["staging", "Staging", "預備環境"],
            ["preview", "Preview", "預覽環境"], ["development", "Development", "開發環境"],
            ["all", "All evidence", "所有證據"]
        ], state.environment);
        options("timeline-category", [["", "All categories", "全部類型"],
            ...Object.entries(CATEGORIES).map(([value, row]) => [value, row[1], row[2]])], state.category || "");
    }
    controls();
    function apply(snapshot) {
        if (!snapshot) return;
        if (typeof snapshot.revision === "number" && (state.revision == null || snapshot.revision >= state.revision)) state.revision = snapshot.revision;
        if (typeof snapshot.enabled === "boolean") state.enabled = snapshot.enabled;
        if (typeof snapshot.viewer?.canManage === "boolean") state.canManage = snapshot.viewer.canManage;
        const toggle = elements["settings-timeline-toggle"];
        if (toggle) {
            toggle.disabled = !state.canManage || state.loading;
            toggle.setAttribute("aria-pressed", String(state.enabled));
            toggle.textContent = state.enabled ? words(locale(), "On", "已開啟") : words(locale(), "Off", "已關閉");
        }
        // Timeline mode is independent from Board mode. The shared environment keeps
        // `onMode` for navigation compatibility, but Timeline snapshots must never be
        // applied to BoardControls.
    }
    function drawProject(snapshot) {
        const projected = snapshot.project;
        const project = projected && typeof projected === "object" ? projected : (state.presentation || {});
        elements["timeline-title"].textContent = project.label || project.name || words(locale(), "Project Timeline", "專案時間軸");
        if (elements["timeline-subtitle"]) elements["timeline-subtitle"].textContent = words(locale(), "Production availability, backed by receipts", "以來源憑證核對 production 是否真正可用");
    }
    function historyCoverage(snapshot) {
        const rows = (Array.isArray(snapshot.checkpoints) ? snapshot.checkpoints : [])
            .filter(row => row.projectId === state.projectId);
        const names = {
            pending: ["Older Git history remains; bounded background batches will continue.", "仍有較早 Git 歷史，背景會分批接續。"],
            complete: ["The observed first-parent history is covered; this is not deployment evidence.", "已涵蓋本次觀測的 first-parent 歷史；這不代表已部署。"],
            capacity: ["Git backfill paused at storage capacity; retained history was not removed.", "Git 回填因容量限制暫停；保留的歷史沒有被刪除。"],
            unavailable: ["Git history could not be read or saved; the cursor was not advanced past unconfirmed work.", "Git 歷史讀取或保存失敗；游標不會跳過未確認的紀錄。"],
            shallow: ["Only shallow-clone history is available; older history coverage is unknown.", "目前只有淺層複製的歷史；更早紀錄的涵蓋範圍未知。"],
            unknown: ["Git history coverage is not yet known for this Project.", "此專案的 Git 歷史涵蓋範圍尚未確認。"],
        };
        const messages = [...new Set((rows.length ? rows : [{ historyStatus: "unknown" }])
            .map(row => words(locale(), ...(names[row.historyStatus] || names.unknown))))];
        if ((snapshot.historySourceIssues || []).some(row => row.projectId === state.projectId))
            messages.push(words(locale(), "History checkpoint was not saved; coverage may be stale.", "歷史游標未成功保存；涵蓋資訊可能過期。"));
        if (!snapshot.enabled) messages.push(words(locale(), "Background history ingestion is paused while Timeline is off.", "Timeline 關閉期間，背景歷史匯入暫停。"));
        const cap = snapshot.capacity;
        if (cap && Number.isSafeInteger(cap.entryCount) && cap.entryCount >= 0 && Number.isSafeInteger(cap.entryLimit) && cap.entryLimit > 0)
            messages.push(words(locale(), `Mac-wide Timeline storage: ${cap.entryCount}/${cap.entryLimit} entries.`, `此 Mac 共用 Timeline 容量：${cap.entryCount}/${cap.entryLimit} 筆。`));
        return messages.join(" ");
    }
    function boardPills(parent, entry) {
        const ids = Array.isArray(entry.boardItemIds) ? entry.boardItemIds : [];
        ids.slice(0, 2).forEach(id => {
            const button = element(doc, "button", id, "timeline-board-pill"); button.type = "button";
            button.addEventListener("click", event => { event.stopPropagation(); environment.openBoard(entry.projectId, id); });
            parent.appendChild(button);
        });
        if (ids.length > 2) parent.appendChild(element(doc, "span", "+" + (ids.length - 2), "timeline-board-more"));
    }
    function card(entry) {
        const button = element(doc, "button", null, "timeline-card"); button.type = "button";
        button.dataset.timelineEntry = entry.id;
        const top = element(doc, "span", null, "timeline-card-top"), kind = category(entry, locale());
        top.appendChild(element(doc, "span", kind.icon + " " + kind.label, "timeline-kind"));
        const projected = timelineStatus(entry.projection && entry.projection.status, locale());
        top.appendChild(element(doc, "span", projected.label, "timeline-state timeline-state-" + projected.tone));
        button.appendChild(top);
        button.appendChild(element(doc, "strong", entry.originalTitle, "timeline-card-title"));
        if (entry.summary) button.appendChild(element(doc, "span", entry.summary, "timeline-card-summary"));
        const foot = element(doc, "span", null, "timeline-card-foot"); boardPills(foot, entry);
        const revisions = Array.isArray(entry.sourceRevisions) ? entry.sourceRevisions.length : 0;
        if (revisions) foot.appendChild(element(doc, "span", revisions + (revisions === 1 ? " revision" : " revisions")));
        foot.appendChild(element(doc, "time", clock(eventTime(entry), locale()))); button.appendChild(foot);
        button.addEventListener("click", () => openEntry(entry.id));
        return button;
    }
    function drawEntries(snapshot) {
        const parent = elements["timeline-items"]; clear(parent);
        const entries = Array.isArray(snapshot.entries) ? snapshot.entries : [];
        if (!entries.length) {
            parent.appendChild(element(doc, "p", state.enabled
                ? state.includeUpcoming
                    ? words(locale(), "No records match these filters. Try another environment or category.", "目前篩選沒有符合的紀錄，可切換環境或類型。")
                    : words(locale(), "No deployment or availability records match these filters. Git-only and not-yet-deployed work is hidden by default.", "目前篩選尚無部署或可用性紀錄；僅有 Git 提交、尚未上線的工作預設隱藏。")
                : words(locale(), "Timeline is off. Earlier history is retained.", "Timeline 已關閉；既有歷史仍保留。"), "timeline-empty"));
            if (state.enabled && !state.includeUpcoming) {
                const show = element(doc, "button", words(locale(), "Show Git / not-yet-deployed history", "查看 Git 與未上線紀錄"), "timeline-more");
                show.type = "button"; show.dataset.timelineAction = "show-git-history";
                show.addEventListener("click", () => {
                    state.includeUpcoming = true;
                    if (elements["timeline-upcoming"]) elements["timeline-upcoming"].checked = true;
                    return refresh();
                });
                parent.appendChild(show);
                parent.appendChild(element(doc, "p", words(locale(),
                    "Git history proves a code change, not a production release. This only changes the filter.",
                    "Git 歷史代表程式碼變更，不代表已上線。這個操作只切換篩選，不會重建或修改紀錄。"), "timeline-empty"));
            }
            return;
        }
        let last = null, section;
        entries.forEach(entry => {
            const group = timelineDateGroup(eventTime(entry), new Date(), locale());
            if (group !== last) {
                section = element(doc, "section", null, "timeline-date");
                section.appendChild(element(doc, "h2", group)); parent.appendChild(section); last = group;
            }
            section.appendChild(card(entry));
        });
        if (snapshot.nextCursor) {
            const more = element(doc, "button", words(locale(), "Load more", "載入更多"), "timeline-more"); more.type = "button";
            more.addEventListener("click", () => refresh(snapshot.nextCursor)); parent.appendChild(more);
        }
    }
    function evidenceRow(parent, event) {
        const row = element(doc, "li", null, "timeline-evidence");
        row.appendChild(element(doc, "strong", event.kind));
        const audience = event.target?.audience && event.target.audience !== "all" ? " · " + event.target.audience : "";
        const cohort = typeof event.cohortPercent === "number" ? " · " + event.cohortPercent + "%" : "";
        row.appendChild(element(doc, "span", (event.authority || "unknown") + " · " + event.result + audience + cohort));
        row.appendChild(element(doc, "time", clock(event.effectiveAt, locale()) + " / " + clock(event.observedAt, locale())));
        parent.appendChild(row);
    }
    function drawDetail(entry) {
        const detail = elements["timeline-detail"]; clear(detail);
        if (!entry) { detail.hidden = true; return; }
        detail.hidden = false;
        const back = element(doc, "button", words(locale(), "Back to Timeline", "返回 Timeline"), "timeline-detail-back");
        back.type = "button"; back.addEventListener("click", () => { state.entryId = null; detail.hidden = true; elements["timeline-items"].hidden = false; });
        detail.appendChild(back); detail.appendChild(element(doc, "h2", entry.originalTitle));
        const projected = timelineStatus(entry.projection && entry.projection.status, locale());
        detail.appendChild(element(doc, "p", projected.label + " · " + ((entry.projection && entry.projection.availableTargets) || 0) + "/" + ((entry.projection && entry.projection.requiredTargets) || 0), "timeline-detail-result"));
        const revisions = element(doc, "ul", null, "timeline-revisions");
        (entry.sourceRevisions || []).forEach(revision => {
            const row = element(doc, "li"), url = revision.githubUrl || githubCommitURL(revision.repositoryId, revision.commit);
            if (url) { const link = element(doc, "a", revision.repositoryId + "@" + revision.shortCommit); link.href = url; link.rel = "noreferrer"; row.appendChild(link); }
            else row.appendChild(element(doc, "span", (revision.shortCommit || String(revision.commit || "").slice(0, 8)) + " · " + words(locale(), "No GitHub link", "沒有 GitHub 連結")));
            revisions.appendChild(row);
        });
        detail.appendChild(revisions); const links = element(doc, "div", null, "timeline-detail-board"); boardPills(links, entry); detail.appendChild(links);
        const evidence = element(doc, "ul", null, "timeline-evidence-list"); (entry.events || []).forEach(row => evidenceRow(evidence, row)); detail.appendChild(evidence);
        elements["timeline-items"].hidden = true;
    }
    async function refresh(cursor = null) {
        if (!state.projectId || state.loading) return null;
        controls();
        state.loading = true; status(words(locale(), "Reading Timeline…", "正在讀取 Timeline…"));
        try {
            const answer = await environment.read(state.projectId, state.entryId, cursor,
                state.environment, state.category, state.includeUpcoming);
            const snapshot = answer && answer.timeline;
            if (!snapshot) throw new Error("timeline response is missing");
            const page = Array.isArray(snapshot.entries) ? snapshot.entries : [];
            if (cursor == null) state.entries = page;
            else {
                const seen = new Set(state.entries.map(entry => entry.id));
                state.entries = state.entries.concat(page.filter(entry => !seen.has(entry.id)));
            }
            const rendered = Object.assign({}, snapshot, { entries: state.entries });
            apply(snapshot); drawProject(snapshot); drawEntries(rendered); drawDetail(snapshot.selected);
            status((snapshot.status === "stale" ? words(locale(), "Showing retained history while sources refresh. ", "來源更新中，先顯示保留的歷史。") : "") + historyCoverage(snapshot));
            return snapshot;
        } catch (error) { status(error.message || words(locale(), "Timeline unavailable", "Timeline 無法讀取")); return null; }
        finally { state.loading = false; apply({ enabled: state.enabled, revision: state.revision }); }
    }
    async function openEntry(id) { state.entryId = id; await refresh(); }
    function enter(project, presentation) { if (project) state.projectId = project; if (presentation) state.presentation = presentation; return refresh(); }
    function leave() { state.entryId = null; if (elements["timeline-detail"]) elements["timeline-detail"].hidden = true; if (elements["timeline-items"]) elements["timeline-items"].hidden = false; }
    function escape() { if (state.entryId) { leave(); return true; } environment.openBoard(state.projectId, null); return true; }
    async function toggleMode() {
        if (state.loading || state.revision == null) return;
        if (!state.pendingMode) state.pendingMode = { operation: "set_enabled", requestId: globalThis.crypto.randomUUID(), expectedRevision: state.revision, enabled: !state.enabled };
        state.loading = true;
        let changed = false, refreshAfterRefusal = false;
        try { const answer = await environment.command(state.pendingMode); state.pendingMode = null; apply(answer.timeline); settingsStatus(""); changed = true; }
        catch (error) {
            const uncertain = !error?.code || /offline|busy|timeout|unavailable|network|connection|persistence_failed/.test(error.code);
            if (!uncertain) { state.pendingMode = null; refreshAfterRefusal = true; }
            settingsStatus(error.message || words(locale(), "Try again", "請重試"));
        }
        finally { state.loading = false; apply({ enabled: state.enabled, revision: state.revision }); }
        if (changed || refreshAfterRefusal) await refresh();
    }
    elements["timeline-refresh"]?.addEventListener("click", () => refresh());
    elements["timeline-back"]?.addEventListener("click", escape);
    elements["timeline-board-tab"]?.addEventListener("click", () => environment.openBoard(state.projectId, null));
    elements["timeline-environment"]?.addEventListener("change", event => { state.environment = event.target.value; refresh(); });
    elements["timeline-category"]?.addEventListener("change", event => { state.category = event.target.value || null; refresh(); });
    elements["timeline-upcoming"]?.addEventListener("change", event => { state.includeUpcoming = !!event.target.checked; refresh(); });
    elements["settings-timeline-toggle"]?.addEventListener("click", toggleMode);
    elements["settings-timeline-history"]?.addEventListener("click", () => enter());
    return { enter, leave, refresh, escape, state };
}
