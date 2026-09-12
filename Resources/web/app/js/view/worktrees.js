/* A Project's worktree lifecycle is a bounded read model produced by the Mac.
   This renderer never probes Git and deliberately has no cleanup transport. */

const CLASSES = new Set([
    "active_in_use", "landed_identical_residue", "genuinely_unlanded",
    "mixed_conflicted", "task_owned_temporary", "prunable_stale_metadata",
    "unknown_incomplete_evidence"
]);
const CONVERSATION = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;
const WORKTREE_ID = /^wt-[0-9a-f]{24}$/i;

function zh(language) { return /^zh/i.test(language || ""); }
function words(language, en, chinese) { return zh(language) ? chinese : en; }
function clear(node) { while (node && node.firstChild) node.removeChild(node.firstChild); }
function node(doc, parent, tag, text, className) {
    const value = doc.createElement(tag);
    if (className) value.className = className;
    if (text !== null && text !== undefined) value.textContent = text;
    parent.appendChild(value);
    return value;
}
function count(value, complete = true) {
    return complete && Number.isSafeInteger(value) && value >= 0
        ? new Intl.NumberFormat().format(value) : "—";
}
function date(value, language) {
    if (!value) return words(language, "not observed", "尚未觀測");
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) return words(language, "invalid time", "時間格式無效");
    return parsed.toLocaleString(language || undefined);
}
function observation(value, language) {
    const state = value && ["current", "stale", "unknown", "failed"].includes(value.state)
        ? value.state : "unknown";
    const label = {
        current: words(language, "current", "最新"),
        stale: words(language, "stale", "已過期"),
        unknown: words(language, "unknown", "未知"),
        failed: words(language, "failed", "讀取失敗")
    }[state];
    return { state, text: label + " · " + date(value && value.observedAt, language) };
}
function classWords(value, language) {
    const labels = {
        active_in_use: ["In use", "使用中"],
        landed_identical_residue: ["Landed; identical residue", "已落地、內容相同的殘留工作樹"],
        genuinely_unlanded: ["Unlanded work", "尚未落地的工作"],
        mixed_conflicted: ["Mixed or conflicted", "內容混合或衝突"],
        task_owned_temporary: ["Task-owned temporary", "任務擁有的暫存工作樹"],
        prunable_stale_metadata: ["Stale metadata", "可修剪的過期 metadata"],
        unknown_incomplete_evidence: ["Evidence incomplete", "證據不完整"]
    };
    const pair = labels[value] || labels.unknown_incomplete_evidence;
    return words(language, pair[0], pair[1]);
}

export function worktreeRowPresentation(row, environment = {}) {
    row = row && typeof row === "object" ? row : {};
    const language = environment.language || "en";
    const known = Array.isArray(row.classifications)
        ? row.classifications.filter(value => CLASSES.has(value)) : [];
    const unknownClass = !Array.isArray(row.classifications)
        || row.classifications.some(value => !CLASSES.has(value));
    const classes = [...new Set(known)];
    if (row.active === true && !classes.includes("active_in_use")) classes.push("active_in_use");
    const statusComplete = row.status && row.status.complete === true;
    const local = observation(row.localObservation, language);
    const canonical = observation(row.canonicalTargetObservation, language);
    if ((unknownClass || classes.length === 0 || !statusComplete
        || local.state === "unknown" || local.state === "failed"
        || canonical.state === "unknown" || canonical.state === "failed")
        && !classes.includes("unknown_incomplete_evidence")) {
        classes.push("unknown_incomplete_evidence");
    }
    const owner = row.owner && typeof row.owner === "object" ? row.owner : null;
    const title = owner && typeof owner.title === "string" && owner.title.trim()
        ? owner.title.trim() : words(language, "Owner not identified", "尚未確認負責者");
    const machine = typeof environment.machine === "string" && environment.machine
        ? environment.machine : null;
    const session = owner && typeof owner.sessionId === "string"
        && CONVERSATION.test(owner.sessionId) ? owner.sessionId.toLowerCase() : null;
    const actionable = classes.some(value => ["active_in_use", "genuinely_unlanded",
        "mixed_conflicted"].includes(value));
    const group = actionable ? "needs" : classes.includes("unknown_incomplete_evidence")
        ? "unknown" : "after";
    return {
        id: typeof row.worktreeId === "string" ? row.worktreeId : "",
        path: typeof row.path === "string" ? row.path : "",
        branch: typeof row.branch === "string" && row.branch ? row.branch
            : words(language, "No branch", "沒有分支"),
        target: typeof row.target === "string" && row.target ? row.target
            : words(language, "Target unknown", "目標未知"),
        head: typeof row.head === "string" ? row.head : null,
        owner: title,
        ownerLocator: machine && session ? { machine, session } : null,
        active: typeof row.active === "boolean" ? row.active : null,
        counts: {
            staged: count(row.status && row.status.staged, statusComplete),
            modified: count(row.status && row.status.modified, statusComplete),
            untracked: count(row.status && row.status.untracked, statusComplete)
        },
        classifications: classes,
        labels: classes.map(value => classWords(value, language)),
        local, canonical, group,
        cleanupEligible: !!(row.cleanup && row.cleanup.eligible === true),
        blockers: Array.isArray(row.cleanup && row.cleanup.blockers)
            ? row.cleanup.blockers.map(value => value && (value.message || value.code)).filter(Boolean)
            : [],
        nextOwner: row.cleanup && typeof row.cleanup.nextOwner === "string"
            ? row.cleanup.nextOwner : null,
        canClean: false
    };
}

function fact(doc, parent, key, value) {
    const line = node(doc, parent, "div", null, "worktree-fact");
    node(doc, line, "span", key, "worktree-fact-key");
    node(doc, line, "span", value, "worktree-fact-value");
}

function renderCard(doc, parent, row, environment) {
    const language = environment.language || "en";
    const view = worktreeRowPresentation(row, environment);
    const card = node(doc, parent, "article", null, "worktree-card is-" + view.group);
    card.dataset.worktreeId = view.id;
    const head = node(doc, card, "div", null, "worktree-card-head");
    const identity = node(doc, head, "div", null, "worktree-identity");
    node(doc, identity, "strong", view.branch, "worktree-branch");
    node(doc, identity, "span", view.path || view.id, "worktree-path");
    if (view.ownerLocator && typeof environment.onOwner === "function") {
        const owner = node(doc, head, "button", view.owner, "worktree-owner-link");
        owner.type = "button";
        owner.addEventListener("click", () => environment.onOwner(view.ownerLocator));
    } else node(doc, head, "span", view.owner, "worktree-owner");
    const tags = node(doc, card, "div", null, "worktree-tags");
    view.labels.forEach(label => node(doc, tags, "span", label, "worktree-tag"));
    const counts = node(doc, card, "div", null, "worktree-counts");
    fact(doc, counts, words(language, "Staged", "已暫存"), view.counts.staged);
    fact(doc, counts, words(language, "Modified", "已修改"), view.counts.modified);
    fact(doc, counts, words(language, "Untracked", "未追蹤"), view.counts.untracked);
    const observations = node(doc, card, "div", null, "worktree-observations");
    fact(doc, observations, words(language, "Local observation", "本機觀測"), view.local.text);
    fact(doc, observations, words(language, "Canonical target", "正式目標"), view.canonical.text);
    fact(doc, observations, words(language, "Target", "目標分支"), view.target);
    const assessment = node(doc, card, "p", null, "worktree-cleanup-assessment");
    if (view.blockers.length) assessment.textContent = view.blockers.join(" · ");
    else if (view.cleanupEligible) assessment.textContent = words(language,
        "The Mac reports this row eligible, but cleanup is unavailable in the browser.",
        "Mac 回報此列符合清理條件，但瀏覽器不提供清理功能。");
    else assessment.textContent = words(language,
        "Cleanup is unavailable until the Mac has complete evidence.",
        "Mac 取得完整證據前，清理功能不可用。");
    if (view.nextOwner) node(doc, card, "p",
        words(language, "Next owner: ", "下一位負責者：") + view.nextOwner,
        "worktree-next-owner");
    return card;
}

export function renderWorktreeRows(container, rows, environment = {}) {
    clear(container);
    const doc = environment.document || document;
    const language = environment.language || (doc.documentElement && doc.documentElement.lang) || "en";
    const groups = [
        ["needs", words(language, "Needs attention", "需要處理")],
        ["after", words(language, "After delivery", "交付之後")],
        ["unknown", words(language, "Evidence incomplete", "證據不完整")]
    ];
    const projected = (Array.isArray(rows) ? rows : []).map(row => ({ row,
        view: worktreeRowPresentation(row, { ...environment, language }) }));
    groups.forEach(([group, title]) => {
        const selected = projected.filter(value => value.view.group === group);
        if (!selected.length) return;
        const section = node(doc, container, "section", null, "worktree-group is-" + group);
        const heading = node(doc, section, "h3", title, "worktree-group-title");
        heading.dataset.count = String(selected.length);
        const list = node(doc, section, "div", null, "worktree-list");
        selected.forEach(value => renderCard(doc, list, value.row, { ...environment, language }));
    });
}

function refusal(error, language) {
    const code = error && error.code;
    const message = error && error.message;
    return (message || words(language, "Worktree status could not be read.",
        "無法讀取工作樹狀態。")) + (code ? " (" + code + (error.status ? " · " + error.status : "") + ")" : "");
}

export function bindWorktreeLifecycle(elements, environment = {}) {
    const doc = environment.document || document;
    const language = () => (doc.documentElement && doc.documentElement.lang) || "en";
    const state = { place: null, answer: null, loading: 0, active: false,
        autoRefreshAttempted: false };

    function needsFirstObservation(answer) {
        const snapshot = answer && answer.projectWorktreeLifecycle;
        return !!(snapshot && snapshot.complete !== true && snapshot.error
            && snapshot.error.code === "not_observed");
    }

    function draw(answer, note) {
        const snapshot = answer && answer.projectWorktreeLifecycle;
        if (!snapshot || snapshot.schemaVersion !== 1 || !Array.isArray(snapshot.rows)
            || typeof answer.machine !== "string" || !answer.machine) {
            throw Object.assign(new Error("Unsupported worktree snapshot"), {
                code: "worktree_schema_unsupported"
            });
        }
        const requestedProject = state.place && (state.place.boardProjectId || state.place.id);
        if (!snapshot.project || snapshot.project.id !== requestedProject) {
            throw Object.assign(new Error("Worktree snapshot belongs to another Project"), {
                code: "worktree_project_mismatch"
            });
        }
        const rowIDs = new Set();
        if (snapshot.rows.some(row => !row || typeof row !== "object"
            || typeof row.worktreeId !== "string" || !WORKTREE_ID.test(row.worktreeId)
            || rowIDs.has(row.worktreeId)
            || !rowIDs.add(row.worktreeId))) {
            throw Object.assign(new Error("Worktree snapshot has an invalid row identity"), {
                code: "worktree_row_identity_invalid"
            });
        }
        state.answer = answer;
        const complete = snapshot.complete === true;
        const counts = snapshot.counts || {};
        elements["project-worktree-summary"].textContent = [
            count(counts.rows) + " " + words(language(), "worktrees", "個工作樹"),
            count(counts.active) + " " + words(language(), "active", "使用中"),
            count(counts.staged) + " " + words(language(), "staged", "已暫存"),
            count(counts.modified) + " " + words(language(), "modified", "已修改"),
            count(counts.untracked) + " " + words(language(), "untracked", "未追蹤"),
            count(counts.unknown) + " " + words(language(), "unknown", "未知")
        ].join(" · ");
        renderWorktreeRows(elements["project-worktree-rows"], snapshot.rows, {
            document: doc, language: language(), machine: answer.machine, local: answer.machine === "this-mac",
            onOwner: locator => environment.openOwner && environment.openOwner(locator, state.place)
        });
        const priorCondition = !complete || snapshot.error
            ? refusal(snapshot.error, language()) : null;
        if (note) elements["project-worktree-status"].textContent = note
            + (priorCondition ? " " + words(language(), "Previous observation: ", "上一次觀測：")
                + priorCondition : "");
        else if (priorCondition) elements["project-worktree-status"].textContent = priorCondition;
        else if (!snapshot.rows.length) elements["project-worktree-status"].textContent = words(language(),
            "Observation complete; no worktrees were found.", "觀測完成；沒有找到工作樹。");
        else elements["project-worktree-status"].textContent = words(language(),
            "Last observed: ", "最近觀測：") + date(snapshot.observedAt, language());
        elements["project-worktree-lifecycle"].dataset.complete = complete ? "true" : "false";
        elements["project-worktree-lifecycle"].dataset.truncated = snapshot.truncated === true ? "true" : "false";
    }

    async function load(refreshing) {
        if (!state.active || !state.place) return;
        const read = refreshing ? environment.refresh : environment.read;
        if (typeof read !== "function") {
            elements["project-worktree-status"].textContent = words(language(),
                "Worktree lifecycle is unavailable on this connection.",
                "這個連線無法讀取工作樹生命週期。");
            return;
        }
        const ticket = ++state.loading;
        elements["project-worktree-refresh"].disabled = true;
        elements["project-worktree-status"].textContent = refreshing
            ? words(language(), "Refreshing the Mac observation…", "正在重新觀測 Mac…")
            : words(language(), "Reading the last worktree observation…", "正在讀取最近一次工作樹觀測…");
        try {
            const answer = await read(state.place);
            if (!state.active || ticket !== state.loading) return;
            draw(answer, refreshing ? words(language(),
                "Observation refreshed. This browser remains read-only.",
                "觀測已更新；瀏覽器仍為唯讀。") : null);
        } catch (error) {
            if (!state.active || ticket !== state.loading) return;
            if (state.answer) {
                draw(state.answer, words(language(), "Showing the previous observation. ",
                    "保留上一次觀測。") + refusal(error, language()));
            } else {
                clear(elements["project-worktree-rows"]);
                elements["project-worktree-summary"].textContent = "";
                elements["project-worktree-status"].textContent = refusal(error, language());
            }
        } finally {
            if (state.active && ticket === state.loading)
                elements["project-worktree-refresh"].disabled = false;
        }
    }

    async function enter(place) {
        state.active = true; state.place = place; state.answer = null;
        state.autoRefreshAttempted = false;
        elements["project-worktree-lifecycle"].hidden = false;
        clear(elements["project-worktree-rows"]);
        elements["project-worktree-summary"].textContent = "";
        const initialTicket = state.loading + 1;
        await load(false);
        // A cached `not_observed` result means the backend has not looked yet, not that the
        // repository has no worktrees. Perform one bounded observation on first entry so the
        // person does not land on an apparently empty page. This deliberately does not retry:
        // typed refusal stays visible and the explicit Refresh button remains the next action.
        if (!state.active || state.place !== place || state.loading !== initialTicket
            || state.autoRefreshAttempted || !needsFirstObservation(state.answer)
            || typeof environment.refresh !== "function") return;
        state.autoRefreshAttempted = true;
        return load(true);
    }
    function leave() {
        state.active = false; state.place = null; state.answer = null; ++state.loading;
        elements["project-worktree-lifecycle"].hidden = true;
        elements["project-worktree-refresh"].disabled = false;
    }
    function refresh() { return load(true); }
    elements["project-worktree-refresh"].addEventListener("click", refresh);
    return { enter, leave, refresh, state };
}
