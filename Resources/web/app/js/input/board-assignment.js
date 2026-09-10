// Assignment is a durable proposal, not a send, resume, or receiver acknowledgement.
const uuid = value => typeof value === "string" && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(value)
    ? value.toLowerCase() : null;
const text = (value, max = 200) => typeof value === "string" && value.length > 0
    && value.length <= max && !/[\x00-\x1f\x7f]/.test(value);
const noteText = value => typeof value === "string" && value.trim().length > 0
    && value.length <= 1000 && !/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/.test(value);
const machineOf = row => row.machine || row.identity?.machine || null;
const sameMachine = (row, machine) => machine ? machineOf(row) === machine
    : !machineOf(row) || machineOf(row) === "this-mac";
const storageKey = "clawdline.board.assignment.v1";
const scopeKey = target => JSON.stringify([target.machine || null, target.projectId, target.itemId]);
const intentKey = record => storageKey + ":" + encodeURIComponent(scopeKey(record.target)) + ":" + record.body.requestId;

export function assignmentCandidates(sessions, { machine, projectPath, owner }) {
    if (!text(projectPath, 4096) || !Array.isArray(sessions)) return [];
    const counts = new Map(), rows = [];
    for (const row of sessions) {
        if (!row || typeof row !== "object" || (row.machine && row.identity?.machine
            && row.machine !== row.identity.machine)) continue;
        const conversation = uuid(row.sessionId), provider = row.assistant;
        if (!sameMachine(row, machine) || !conversation || !["claude", "codex"].includes(provider)) continue;
        const key = provider + ":" + conversation;
        counts.set(key, (counts.get(key) || 0) + 1);
        if (row.cwd !== projectPath || conversation === uuid(owner)) continue;
        rows.push({ key, conversation, provider, title: text(row.title, 1000) ? row.title : conversation });
    }
    return rows.filter(row => counts.get(row.key) === 1)
        .sort((a, b) => a.title.localeCompare(b.title) || a.key.localeCompare(b.key));
}

export function createBoardAssignmentController(env) {
    const state = { status: "closed", candidates: [], selected: null, item: null, error: null };
    let generation = 0, target = null, board = null, pending = null, sending = false;
    const draw = () => env.render?.(state);
    const fail = code => { state.status = "error"; state.error = code; draw(); };
    const storage = () => env.storage || globalThis.localStorage;
    function validIntent(record, key) {
        const b = record?.body, t = record?.target;
        if (!t || Object.keys(record).sort().join() !== "body,target"
            || Object.keys(t).sort().join() !== "itemId,machine,projectId"
            || !text(t.projectId) || !text(t.itemId) || !(t.machine === null || text(t.machine))
            || !b || !uuid(b.requestId) || intentKey(record) !== key
            || !Number.isSafeInteger(b.expectedRevision) || b.expectedRevision < 0
            || b.itemId !== t.itemId || !noteText(b.note)) return false;
        const fields = b.operation === "assign_session"
            ? ["operation", "requestId", "expectedRevision", "itemId", "projectId", "sessionId", "provider", "note"]
            : ["operation", "requestId", "expectedRevision", "itemId", "assignmentId", "note"];
        if (Object.keys(b).sort().join() !== fields.sort().join()) return false;
        return b.operation === "assign_session"
            ? b.projectId === t.projectId && !!uuid(b.sessionId) && ["claude", "codex"].includes(b.provider)
            : b.operation === "cancel_session_assignment" && text(b.assignmentId);
    }
    function retained() {
        const s = storage(), result = new Map(); let bytes = 0;
        // The pre-release aggregate shape is not silently discarded or migrated.
        if (s.getItem(storageKey)) throw Error("legacy journal requires inspection");
        if (!Number.isSafeInteger(s.length) || s.length > 10000) throw Error("storage bound");
        const keys = new Set();
        for (let i = 0; i < s.length; i++) {
            const key = s.key(i); if (key?.startsWith(storageKey + ":")) keys.add(key);
        }
        for (const key of keys) {
            const raw = s.getItem(key);
            if (!raw || raw.length > 65536 || key.length > 4096) throw Error("storage changed");
            bytes += new TextEncoder().encode(key + raw).length;
            const record = JSON.parse(raw);
            if (!validIntent(record, key)) throw Error("storage identity");
            result.set(key, record);
        }
        if (result.size > 32 || bytes > 65536) throw Error("storage bound");
        return result;
    }
    const forTarget = selected => [...retained().values()]
        .filter(r => scopeKey(r.target) === scopeKey(selected))
        .sort((a,b) => a.body.requestId.localeCompare(b.body.requestId));
    function save(record) {
        const key = intentKey(record), raw = JSON.stringify(record), s = storage();
        if (forTarget(record.target).some(r => JSON.stringify(r) !== raw)) throw Error("another intent is pending");
        if (s.getItem(key) && s.getItem(key) !== raw) throw Error("request conflict");
        // Separate immutable per-request keys avoid a cross-tab read/modify/write aggregate.
        // A late settlement can only delete its own exact bytes, never another request.
        s.setItem(key, raw);
        try {
            if (s.getItem(key) !== raw) throw Error("storage lost");
            retained();
        } catch (error) { if (s.getItem(key) === raw) s.removeItem(key); throw error; }
    }
    function settle(record) {
        const key = intentKey(record), raw = storage().getItem(key);
        if (raw === null) return; // Another tab already settled this same request.
        if (raw !== JSON.stringify(record)) throw Error("request changed");
        storage().removeItem(key);
        if (storage().getItem(key) !== null) throw Error("storage retained");
    }
    function validate(answer, selectedTarget, command = false) {
        const b = answer?.board;
        if (!b || b.schemaVersion !== 1 || b.enabled !== true || !Number.isSafeInteger(b.revision)
            || b.revision < 0 || b.item?.id !== selectedTarget.itemId
            || b.item?.projectId !== selectedTarget.projectId
            || !Object.hasOwn(b.item, "sessionAssignment")
            || !Array.isArray(b.projects) || b.projects.filter(p => p.id === selectedTarget.projectId).length !== 1
            || (!command && b.readState?.status !== "ready")
            || (b.readState && (b.readState.status !== "ready" || b.readState.revision !== b.revision)))
            throw { code: "board_unavailable" };
        return b;
    }
    const allowed = () => env.canWrite() && env.inventoryReady();
    function apply(b) {
        const prior = state.project;
        board = b; state.item = b.item; state.project = b.projects.find(p => p.id === target.projectId);
        // Command receipts have a minimal catalog, unlike the read model. Retain only the
        // already-read same-Project path; this does not invent a missing reload identity.
        if (!state.project.displayPath && prior?.id === state.project.id)
            state.project = { ...state.project, displayPath: prior.displayPath };
        state.candidates = assignmentCandidates(env.sessions(), { machine: target.machine,
            projectPath: state.project.displayPath, owner: b.item.owner });
        state.status = "ready"; state.error = null; state.selected = null; draw();
    }
    async function open(selectedTarget) {
        if (sending) return;
        const epoch = ++generation;
        target = { machine: selectedTarget?.machine || null, projectId: selectedTarget?.projectId,
            itemId: selectedTarget?.itemId };
        state.status = "loading"; state.error = null; state.item = null; state.project = null; state.candidates = []; pending = null;
        draw();
        if (!text(target.projectId) || !text(target.itemId) || (env.requireMachine && !text(target.machine)))
            return fail("identity_unavailable");
        try {
            const records = forTarget(target);
            pending = records[0] || null; state.pendingCount = records.length;
            if (pending) { state.status = "uncertain"; draw(); return; }
            if (!allowed()) return fail("write_or_inventory_unavailable");
            const b = validate(await env.read(target.projectId, target.itemId, target.machine || undefined), target);
            if (epoch === generation) apply(b);
        } catch (error) { if (epoch === generation) fail(error.code || "storage_or_read_unavailable"); }
    }
    async function transmit(record) {
        if (sending || !allowed()) return fail("write_or_inventory_unavailable");
        sending = true; const epoch = generation;
        state.status = "sending"; state.error = null; draw();
        try {
            const answer = await env.command(structuredClone(record.body), record.target.machine || undefined);
            const b = validate(answer, record.target, true);
            if (b.revision <= record.body.expectedRevision) throw { code: "receipt_unavailable" };
            settle(record); pending = null;
            if (epoch === generation) {
                apply(b);
                const more = forTarget(target); pending = more[0] || null; state.pendingCount = more.length;
                if (pending) { state.status = "uncertain"; draw(); }
                try { env.changed?.(); } catch (_) { /* Receipt is still durable. */ }
            }
        } catch (error) {
            // A typed deterministic refusal is not an uncertain delivery. Never mint a retry
            // automatically; a new proposal requires another fresh read and user confirmation.
            const definite = (error.status >= 400 && error.status < 500 && ![408,429].includes(error.status))
                || ["revision_conflict", "assignment_project_changed", "handoff_pending", "handoff_same_owner",
                    "assignment_not_pending", "assignment_identity_invalid", "read_only", "cloud_read_only"].includes(error.code);
            if (definite) {
                try { settle(record); pending = null; }
                catch (_) { if (epoch === generation) { state.status = "uncertain"; state.error = "storage_unavailable"; draw(); } return; }
            }
            if (epoch === generation) {
                state.status = definite ? "error" : "uncertain";
                state.error = error.code || "delivery_unconfirmed"; draw();
            }
        } finally { sending = false; }
    }
    async function write(operation, note) {
        if (sending || state.status !== "ready" || !allowed()) return;
        if (!noteText(note)) return fail("note_required");
        const body = { operation, requestId: globalThis.crypto.randomUUID(), expectedRevision: board.revision,
            itemId: target.itemId, note: note.trim() };
        if (operation === "assign_session") {
            const row = assignmentCandidates(env.sessions(), { machine: target.machine,
                projectPath: state.project.displayPath, owner: board.item.owner }).find(r => r.key === state.selected);
            if (!row) return fail("session_unavailable");
            if (board.item.handoff) return fail("handoff_pending");
            Object.assign(body, { projectId: target.projectId, provider: row.provider, sessionId: row.conversation });
        } else {
            const h = board.item.handoff;
            if (!h || h.status !== "pending" || !["claude", "codex"].includes(h.provider)) return fail("assignment_not_pending");
            body.assignmentId = h.id;
        }
        const record = { target: { ...target }, body };
        try { save(record); pending = record; } catch (_) { return fail("storage_unavailable"); }
        await transmit(record);
    }
    return { state, open, select(key) { if (state.status === "ready") state.selected = key; },
        propose: note => write("assign_session", note), cancel: note => write("cancel_session_assignment", note),
        openReceiver() {
            const h = state.item?.handoff;
            const rows = env.sessions() || [];
            const matches = rows.filter(r => r && sameMachine(r, target?.machine)
                && !(r.machine && r.identity?.machine && r.machine !== r.identity.machine)
                && uuid(r.sessionId) === uuid(h?.proposedOwner) && r.assistant === h?.provider
                && r.cwd === state.project?.displayPath);
            if (state.status !== "ready" || !env.inventoryReady() || !uuid(h?.proposedOwner)
                || !["claude", "codex"].includes(h?.provider) || matches.length !== 1
                || !text(matches[0].id) || rows.filter(r => r?.id === matches[0].id).length !== 1
                || !env.openLive) return fail("receiver_unavailable");
            // One synchronous observed identity handoff; no untyped history/resume fallback.
            env.openLive(matches[0].id); ++generation; state.status = "closed"; draw();
        },
        retry: async () => { if (!sending && state.status === "uncertain" && pending) await transmit(pending); },
        close() { ++generation; state.status = "closed"; draw(); } };
}

export function bindBoardAssignment(doc, env) {
    const dialog = doc.createElement("dialog");
    dialog.className = "board-assignment-dialog"; dialog.setAttribute("aria-label", "Session assignment");
    doc.body.appendChild(dialog);
    const zh = () => (doc.documentElement.lang || "").toLowerCase().startsWith("zh");
    const words = (en, tw) => zh() ? tw : en;
    let target = null;
    const node = (tag, label, action) => {
        const n = doc.createElement(tag); if (label) n.textContent = label;
        if (action) n.dataset.assignmentAction = action;
        dialog.appendChild(n); return n;
    };
    const button = (label, action, fn, disabled = false) => {
        const n = node("button", label, action); n.type = "button"; n.disabled = disabled;
        n.addEventListener("click", fn); return n;
    };
    const controller = createBoardAssignmentController({ ...env, render(state) {
        env.render?.(state);
        if (state.status === "closed") { if (dialog.open) dialog.close(); return; }
        dialog.textContent = "";
        node("h2", words("Assign a Session", "指派 Session"));
        node("p", words("This records a proposal. It does not send a message or start work. The current owner remains responsible until the receiver explicitly accepts.",
            "這裡只記錄指派提案，不會送訊息或啟動工作。對方明確接受之前，原負責人仍負責。"));
        if (state.item) node("h3", state.item.title);
        if (state.status === "loading" || state.status === "sending")
            node("p", words("Waiting for the selected Mac…", "正在等待所選 Mac 回覆…"));
        if (state.status === "uncertain") {
            node("p", words("Delivery is unconfirmed. Reloading does not resend. Retry checks the same request, without creating a second proposal.",
                "尚未確認是否已記錄；重新整理不會重送。重試會沿用同一筆請求，不另建提案。"));
            button(words("Retry the same request", "重試同一筆請求"), "retry", () => controller.retry());
        }
        if (state.error) {
            const error = node("p", words("Cannot confirm this change. No takeover is claimed. Refresh the item or restore the connection before trying again.",
                "目前無法確認變更，未宣稱接手成功。請重新讀取項目或恢復連線後再試。"));
            error.setAttribute("role", "status");
            node("code", state.error);
        }
        if (state.status === "error")
            button(words("Read again", "重新讀取"), "refresh", () => controller.open(target));
        if (state.status === "ready") {
            const handoff = state.item.handoff;
            if (handoff) {
                node("p", handoff.provider && handoff.status === "pending"
                    ? words("Awaiting acceptance", "待接受") : words("Legacy handoff", "舊式交接"));
                node("p", words("This picker does not send notifications; other notification activity is not observed here.",
                    "此介面不會通知接收方；其他人是否已通知，這裡沒有觀測紀錄。"));
                const receiver = state.candidates.find(r => r.conversation === uuid(handoff.proposedOwner)
                    && r.provider === handoff.provider);
                node("p", receiver?.title || handoff.proposedOwner);
                node("p", handoff.note);
                if (handoff.provider && handoff.status === "pending") {
                    button(words("Open observed receiver Session", "開啟已觀測的接收方 Session"), "open-receiver",
                        () => controller.openReceiver(), !env.openLive);
                    button(words("Copy takeover context", "複製接手說明"), "copy-context", async () => {
                        const context = words("Please review this Board assignment; accept or decline explicitly using your own process-bound managed workflow.",
                            "請檢視這筆看板指派，並透過你自己的 process-bound managed workflow 明確接受或拒絕。")
                            + "\n" + JSON.stringify({ machine: target.machine, project_id: target.projectId,
                                item_id: target.itemId, assignment_id: handoff.id, provider: handoff.provider,
                                conversation_id: handoff.proposedOwner, note: handoff.note }, null, 2);
                        try { await env.copy(context); }
                        catch (_) { node("p", words("Copy failed; nothing was sent.", "複製失敗；未送出任何內容。")); }
                    }, !env.copy);
                    button(words("Withdraw this proposal", "撤回這筆提案"), "cancel", () => controller.cancel(
                        words("Withdrawn in Board assignment picker", "由看板指派介面撤回")));
                } else node("p", words("This is a legacy handoff; this picker cannot settle it.", "這是舊式交接紀錄，不能由此介面決定。"));
            } else {
                if (state.item.sessionAssignment) {
                    const labels = { accepted: ["Accepted", "已接受"], declined: ["Declined", "已拒絕"], cancelled: ["Withdrawn", "已撤回"] };
                    const label = labels[state.item.sessionAssignment.status];
                    node("p", words("Last assignment: ", "上一筆指派：") + (label ? words(...label) : words("Unknown", "未知")));
                }
                const label = node("label", words("Receiver (observed in this Project)", "接收方（此專案已觀測到的 Session）"));
                const select = doc.createElement("select"); select.dataset.assignmentAction = "receiver"; label.appendChild(select);
                const empty = doc.createElement("option"); empty.value = "";
                empty.textContent = words("Choose a Session", "選擇 Session"); select.appendChild(empty);
                for (const row of state.candidates) {
                    const option = doc.createElement("option"); option.value = row.key;
                    option.textContent = row.title + " · " + row.provider + " · " + row.conversation;
                    select.appendChild(option);
                }
                select.addEventListener("change", () => controller.select(select.value));
                const noteLabel = node("label", words("What should they handle?", "希望對方處理什麼？"));
                const note = doc.createElement("textarea"); note.dataset.assignmentAction = "note";
                note.maxLength = 1000; note.rows = 3; noteLabel.appendChild(note);
                if (!state.candidates.length) node("p", words("No uniquely identified Session in this Project is available. No Session will be started automatically.",
                    "目前沒有可唯一識別的同專案 Session；不會自動開啟新 Session。"));
                button(words("Confirm proposal", "確認指派提案"), "propose", () => controller.propose(note.value), !state.candidates.length);
            }
            button(words("Refresh status", "更新狀態"), "refresh", () => controller.open(target));
        }
        button(words("Close", "關閉"), "close", () => controller.close());
        if (!dialog.open) dialog.showModal();
    }});
    dialog.addEventListener("cancel", event => { event.preventDefault(); controller.close(); });
    return { ...controller, open(selected) { target = { ...selected }; return controller.open(selected); } };
}
