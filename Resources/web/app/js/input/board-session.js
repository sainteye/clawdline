/** Board -> Session uses observed identities; reading this sheet never starts a process. */
export function createBoardSessionController(env) {
    const state = { status: "closed", conversation: null, project: null, candidate: null, error: null };
    let generation = 0, reading = null, sending = false;
    const storageKey = "clawdline.board.resume-fences.v1";
    const storage = () => env.storage || globalThis.localStorage;
    function fences() {
        try {
            const raw = storage().getItem(storageKey);
            if (raw && raw.length > 131072) throw Error("resume fence capacity");
            const rows = raw ? JSON.parse(raw) : [];
            if (!Array.isArray(rows) || rows.length > 64) throw Error("invalid resume fence");
            const result = new Map();
            for (const row of rows) {
                // Retain legacy unknown fences; absence of their original request is not
                // evidence that a new action is safe.
                const pair = typeof row === "string" ? [row, null] : row;
                if (!Array.isArray(pair) || pair.length !== 2 || typeof pair[0] !== "string"
                    || pair[0].length > 2048 || (pair[1] !== null
                        && (typeof pair[1] !== "string" || !/^[0-9a-f-]{36}$/i.test(pair[1])))) {
                    throw Error("invalid resume fence");
                }
                const fields = JSON.parse(pair[0]);
                if (!Array.isArray(fields) || fields.length !== 4
                    || fields.some(field => typeof field !== "string")) throw Error("invalid resume fence");
                result.set(pair[0], pair[1]);
            }
            return result;
        } catch (_) { throw { code: "resume_storage_unavailable" }; }
    }
    function persist(rows) {
        if (rows.size > 64) throw Error("resume fence capacity");
        const encoded = JSON.stringify([...rows]);
        storage().setItem(storageKey, encoded);
        if (storage().getItem(storageKey) !== encoded) throw Error("resume fence not retained");
    }
    const fenceKey = (selected, conversation) => JSON.stringify([
        selected.place.machine || "", selected.place.id, selected.assistant, conversation.toLowerCase()
    ]);
    const draw = () => env.render(state);
    const live = id => (env.sessions() || []).filter(row => row.sessionId === id);
    function observe() {
        try {
            const rows = fences(); let changed = false;
            for (const key of rows.keys()) {
                const [machine, , provider, conversation] = JSON.parse(key);
                const matches = live(conversation).filter(row => row.assistant === provider
                    && (machine ? (row.machine || row.identity?.machine) === machine
                        : !row.machine || row.machine === "this-mac"));
                if (matches.length === 1) { rows.delete(key); changed = true; }
            }
            if (changed) persist(rows);
        } catch (_) { /* Retain unknown fences when storage cannot be read or settled. */ }
    }
    function openObserved(conversation, row) {
        // Admission/UI handoff is not observation. Only a unique live inventory row can
        // settle this fence, allowing a later resume after that observed Session ends.
        observe();
        close(); env.openLive(row.id);
    }
    function fail(code) { state.status = "error"; state.error = code; draw(); }
    async function open(conversation, project) {
        if (sending) return;
        const matches = live(conversation);
        if (matches.length === 1) { openObserved(conversation, matches[0]); return; }
        const ticket = ++generation;
        Object.assign(state, { status: "loading", conversation, project, candidate: null, error: null });
        draw();
        if (matches.length > 1) { fail("session_ambiguous"); return; }
        // Fence the previous result, but keep its physical read single-flight.
        // A live Session above must remain reachable while history is slow.
        if (reading) { fail("read_pending"); return; }
        if (!/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(conversation || "")
            || !project || !project.displayPath) { fail("project_unavailable"); return; }
        reading = (async () => {
            try {
                const inventory = await env.places();
                if (ticket !== generation) return;
                const places = (inventory.places || []).filter(p => p.path === project.displayPath);
                // A path on two Macs is not a unique destination. Never fall back to a label.
                if (places.length !== 1) { fail(places.length ? "project_ambiguous" : "project_unavailable"); return; }
                const providers = (inventory.assistants || []).map(a => a.id)
                    .filter((id, index, ids) => ["claude", "codex"].includes(id) && ids.indexOf(id) === index);
                const found = [];
                let incomplete = false;
                // At most two narrow history reads, only after a deliberate click.
                for (const assistant of providers) {
                    const answer = await env.history(places[0].id, assistant);
                    if (ticket !== generation) return;
                    incomplete ||= answer.more === true;
                    for (const row of answer.sessions || []) {
                        if (row.id === conversation) found.push({ row, assistant, place: places[0] });
                    }
                }
                if (found.length !== 1) { fail(found.length ? "session_ambiguous" : incomplete ? "history_incomplete" : "history_unavailable"); return; }
                state.candidate = found[0];
                state.status = found[0].row.live ? "live_unobserved"
                    : fences().has(fenceKey(found[0], conversation)) ? "uncertain" : "ready";
                draw();
            } catch (error) {
                if (ticket === generation) fail(error.code || "read_failed");
            } finally { reading = null; }
        })();
        return reading;
    }
    async function resume() {
        if (sending || state.status !== "ready" || !state.candidate || !env.canWrite()) return;
        const matches = live(state.conversation);
        if (matches.length === 1) { openObserved(state.conversation, matches[0]); return; }
        if (matches.length > 1) { fail("session_ambiguous"); return; }
        const selected = state.candidate, conversation = state.conversation;
        const key = fenceKey(selected, conversation);
        let requestId;
        try {
            const rows = fences();
            if (rows.has(key)) { state.status = "uncertain"; draw(); return; }
            requestId = globalThis.crypto.randomUUID();
            rows.set(key, requestId); persist(rows);
        } catch (_) { fail("resume_storage_unavailable"); return; }
        sending = true; state.status = "resuming"; draw();
        // An ambiguous transport failure must not offer a second terminal mutation.
        let accepted = false;
        try {
            const answer = await env.resume(selected.place.id, conversation, selected.assistant, requestId);
            if (!answer || !answer.id) { state.status = "uncertain"; draw(); return; }
            accepted = true;
            state.status = "closed"; draw();
            env.began(answer, { ...selected.place, label: selected.row.title });
        } catch (error) {
            // Only explicit server refusals prove no terminal was launched.
            // A failed UI handoff after an accepted reply is not a server refusal.
            if (!accepted && ["write_disabled", "forbidden", "not_found", "bad_request", "terminal_unsupported"].includes(error.code)) {
                try { const rows = fences(); rows.delete(key); persist(rows); } catch (_) { /* Safe to retain. */ }
                fail(error.code);
            } else { state.status = "uncertain"; state.error = error.code || "delivery_unknown"; draw(); }
        } finally { sending = false; }
    }
    function close() {
        if (sending) return;
        generation++; state.status = "closed"; state.candidate = null; draw();
    }
    return { state, open, resume, close, observe };
}

export const BoardSession = { observe: function () {} };

export function bindBoardSession(document, env) {
    const dialog = document.createElement("dialog");
    dialog.className = "board-session-dialog";
    dialog.setAttribute("aria-label", "Session");
    document.body.appendChild(dialog);
    let focusBefore = null;
    const words = (en, zh) => /^zh/i.test(document.documentElement.lang || "") ? zh : en;
    const messages = {
        loading: ["Finding this conversation…", "正在尋找這段對話…"],
        ready: ["This conversation is not running. Resume opens it on your Mac; nothing has started yet.", "這段對話目前沒有執行。按下「繼續對話」才會在 Mac 恢復；目前尚未啟動。"],
        resuming: ["Resume requested. Do not submit again.", "正在恢復對話，請勿重複提交。"],
        uncertain: ["The result has not been confirmed. Check the Session list before trying again; no automatic retry was sent.", "尚未確認恢復結果。請先查看 Session 列表，避免重複啟動；系統不會自動重送。"],
        live_unobserved: ["The Mac reports this conversation is already running. Refresh the Session list to open it.", "Mac 回報這段對話正在執行；請更新 Session 列表後開啟，避免重複恢復。"]
    };
    const errors = {
        session_ambiguous: ["More than one Session matches; no destination was guessed.", "有多個 Session 符合，未猜測或啟動任何對話。"],
        project_ambiguous: ["This Project exists on more than one Mac. Open it from the desired Mac's Session list.", "多台 Mac 有同一路徑的專案；請從目標 Mac 的 Session 列表開啟。"],
        project_unavailable: ["The original Project is unavailable in this Mac's inventory.", "目前的 Mac 專案清單中找不到原專案。"],
        history_unavailable: ["The recorded conversation could not be found. Its Board record is preserved.", "找不到這段歷史對話；看板上的工作紀錄仍保留。"],
        history_incomplete: ["This conversation was not in the bounded history result; it may still exist.", "本次有限範圍的歷史讀取沒有列到這段對話，不代表已刪除。"],
        read_failed: ["Could not read the history. Nothing was started.", "暫時無法讀取歷史，未啟動任何對話。"],
        read_pending: ["The previous history read is still settling. Close this sheet and try again shortly; nothing was started.", "上一筆歷史讀取尚未結束，請稍後關閉再試；未啟動任何對話。"],
        write_disabled: ["Remote control is disabled on this Mac.", "這台 Mac 未開啟遠端操作。"],
        forbidden: ["This device is not allowed to resume the conversation. Check its remote-control permissions.", "此裝置沒有恢復對話的權限，請檢查遠端操作權限。"],
        not_found: ["The Mac no longer recognizes this Project or conversation. Refresh the Session list.", "Mac 已找不到此專案或對話，請更新 Session 列表。"],
        bad_request: ["The Mac refused this resume request. Refresh the Project before trying again.", "Mac 拒絕這筆恢復要求，請先更新專案資料。"],
        terminal_unsupported: ["This terminal cannot resume conversations. Use a supported terminal on the Mac.", "此終端不支援恢復對話，請在 Mac 使用支援的終端。"],
        resume_storage_unavailable: ["The browser cannot retain a safe resume receipt. Nothing was started; check browser storage availability.", "瀏覽器無法保存防重複啟動紀錄，未啟動對話；請檢查瀏覽器儲存空間與權限。"]
    };
    function render(state) {
        if (state.status === "closed") {
            if (dialog.open) { dialog.close(); if (focusBefore && focusBefore.isConnected) focusBefore.focus(); }
            return;
        }
        dialog.textContent = "";
        const heading = document.createElement("h2");
        heading.textContent = state.candidate ? state.candidate.row.title : words("Related conversation", "關聯對話");
        dialog.appendChild(heading);
        const subtitle = document.createElement("p");
        subtitle.textContent = state.project && (state.project.label || state.project.name) || "";
        dialog.appendChild(subtitle);
        const status = document.createElement("p"); status.setAttribute("role", "status");
        status.textContent = words(...(state.status === "error" ? errors[state.error] || errors.read_failed : messages[state.status]));
        dialog.appendChild(status);
        if (state.status === "ready") {
            const resume = document.createElement("button"); resume.type = "button";
            resume.textContent = words("Resume conversation", "繼續對話"); resume.disabled = !env.canWrite();
            resume.addEventListener("click", () => controller.resume()); dialog.appendChild(resume);
        }
        const close = document.createElement("button"); close.type = "button";
        close.textContent = words("Close", "關閉"); close.disabled = state.status === "resuming";
        close.addEventListener("click", () => controller.close()); dialog.appendChild(close);
        if (!dialog.open) { focusBefore = document.activeElement; dialog.showModal(); }
    }
    const controller = createBoardSessionController({ ...env, render });
    BoardSession.observe = controller.observe;
    dialog.addEventListener("cancel", event => { event.preventDefault(); controller.close(); });
    return controller;
}
