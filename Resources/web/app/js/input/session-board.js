/** Optional Session context. It never gates transcript, send or attachment delivery. */
export function createSessionBoardController(env) {
    var state = { enabled: false, session: null, status: "idle", rows: [], projects: [], truncated: false };
    var generation = 0, flight = null, wanted = false, attempted = false;
    function draw() { env.render(state); }
    function load(force) {
        if (!state.enabled || !state.session || !env.visible() || (attempted && !force)) return Promise.resolve();
        if (flight) { wanted = true; return flight; }
        attempted = true; wanted = false;
        var ticket = generation, session = state.session.sessionId;
        state.status = "loading"; draw();
        flight = Promise.resolve().then(function () { return env.read(null, "session:" + session); })
            .then(function (answer) {
                if (ticket !== generation || !state.enabled) return;
                var board = answer && answer.board;
                if (board && board.enabled === false) { controller.setEnabled(false); return; }
                if (!board || board.sessionId !== session || !Array.isArray(board.items)) {
                    throw new Error("session_relations_unavailable");
                }
                state.rows = board.items.filter(function (row) {
                    return row && typeof row.id === "string" && typeof row.projectId === "string";
                }).slice(0, 100);
                state.projects = board.projects || [];
                state.truncated = board.truncated === true;
                state.status = board.readState && board.readState.status !== "ready" ? "stale" : "ready";
            }).catch(function () {
                if (ticket === generation) state.status = "error";
            }).finally(function () {
                flight = null;
                if (ticket === generation) draw();
                if (wanted) { wanted = false; load(true); }
            });
        return flight;
    }
    var controller = {
        state: state,
        follow: function (session) {
            // A terminal identity alone is not a provider conversation and may be reused.
            var valid = session && typeof session.sessionId === "string"
                && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(session.sessionId);
            generation++; attempted = false; wanted = false;
            state.session = valid ? { ...session, sessionId: session.sessionId.toLowerCase() } : null;
            state.rows = []; state.projects = []; state.status = "idle"; state.truncated = false;
            draw();
        },
        setEnabled: function (enabled) {
            if (state.enabled === (enabled === true)) return;
            generation++; attempted = false; wanted = false;
            state.enabled = enabled === true;
            state.rows = []; state.projects = []; state.status = "idle";
            draw();
        },
        load: load,
        open: function (id) {
            if (!state.enabled || !state.session) return;
            var row = state.rows.find(function (item) { return item.id === id; });
            if (!row) return;
            env.open(row.projectId, row.id, state.projects.find(function (project) { return project.id === row.projectId; }));
        }
    };
    return controller;
}

export var SessionBoard = { follow: function () {}, sync: function () {}, resume: function () {}, setEnabled: function () {} };

export function bindSessionBoard(container, env) {
    var doc = container.ownerDocument, expanded = false, followed = "", wasVisible = false;
    var heading = doc.createElement("button"), body = doc.createElement("div");
    heading.type = "button"; heading.className = "session-board-heading";
    body.id = "session-board-relations"; body.className = "session-board-relations";
    heading.setAttribute("aria-controls", body.id);
    container.appendChild(heading); container.appendChild(body);
    function words(en, zh) { return /^zh/i.test(doc.documentElement.lang || "") ? zh : en; }
    function draw(state) {
        container.hidden = !state.enabled || !state.session || !env.visible();
        var first = state.rows.find(function (row) { return row.sessionActivity === "declared"; }) || state.rows[0];
        heading.textContent = words("Board items", "看板項目") + (first ? " · " + first.title : "")
            + (state.rows.length > 1 ? " (+" + (state.rows.length - 1) + ")" : "");
        heading.setAttribute("aria-expanded", String(expanded));
        body.hidden = !expanded;
        body.textContent = "";
        var status = doc.createElement("p");
        status.setAttribute("role", "status");
        if (state.status === "loading") status.textContent = words("Reading related work…", "正在讀取關聯工作…");
        else if (state.status === "error") status.textContent = words("Related work could not be read. Your conversation is unaffected.", "暫時無法讀取關聯工作，不影響對話。");
        else if (state.status === "stale") status.textContent = words("Showing the last observed relationships.", "目前顯示上次觀測到的關聯。");
        else if (state.status === "ready" && !state.rows.length) status.textContent = words("No recorded Board relationship yet.", "目前尚未記錄看板關聯。");
        body.appendChild(status);
        state.rows.forEach(function (row) {
            var button = doc.createElement("button"), project = state.projects.find(function (p) { return p.id === row.projectId; });
            button.type = "button";
            button.textContent = (row.sessionActivity === "declared" ? words("Reported activity · ", "已登記活動 · ") : "")
                + (row.key || "") + " " + row.title + " · " + (project && (project.label || project.name) || words("Project", "專案"));
            button.addEventListener("click", function () { controller.open(row.id); });
            body.appendChild(button);
        });
        if (state.truncated) {
            var note = doc.createElement("p");
            note.textContent = words("More relationships exist; open the Project for the full list.", "另有未列出的關聯；請進入專案查看。");
            body.appendChild(note);
        }
        var refresh = doc.createElement("button");
        refresh.type = "button"; refresh.textContent = words("Refresh relationships", "更新關聯");
        refresh.disabled = state.status === "loading";
        refresh.addEventListener("click", function () { controller.load(true); });
        body.appendChild(refresh);
    }
    var controller = createSessionBoardController({ ...env, render: draw });
    heading.addEventListener("click", function () {
        expanded = !expanded; draw(controller.state);
        if (expanded) controller.load(false);
    });
    function identity(session) { return session ? session.id + ":" + (session.sessionId || "") : ""; }
    SessionBoard.follow = function (session) {
        followed = identity(session); expanded = false; controller.follow(session);
    };
    SessionBoard.sync = function (session) {
        var changed = followed !== identity(session), visible = env.visible();
        if (changed) SessionBoard.follow(session);
        if (wasVisible !== visible) { wasVisible = visible; draw(controller.state); }
        // Late provider identity is normal just after launch. Observe it without making the
        // inventory renderer join the optional read or repeatedly polling on every frame.
        if (changed && visible && env.ready && env.ready()) Promise.resolve().then(() => controller.load(false));
    };
    SessionBoard.resume = function () { return controller.load(false); };
    SessionBoard.setEnabled = function (value) {
        controller.setEnabled(value);
        if (value && env.ready && env.ready()) Promise.resolve().then(() => controller.load(false));
    };
    draw(controller.state);
    return controller;
}
