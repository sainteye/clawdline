import { api } from "../net/api.js";
import { Pages } from "../core/pages.js";

function words(en, zh) {
    return /^zh/i.test(document.documentElement.lang || navigator.language || "") ? zh : en;
}
function node(id) { return document.getElementById(id); }
var latest = null, reading = null, pending = null, bound = false, saving = false, operationError = false;
function canManage() { return !!(latest && latest.viewer && latest.viewer.canManage === true); }

export var BoardControls = {
    escape: function () { Pages.goHome(); },
    open: function () { Pages.go("board"); },
    apply: function (board) {
        if (!board || typeof board.enabled !== "boolean") return;
        if (latest && board.revision < latest.revision) return;
        if (!board.viewer && latest) board = Object.assign({}, board, { viewer: latest.viewer });
        latest = board;
        document.documentElement.dataset.boardMode = board.enabled ? "board" : "standard";
        ["nav-projects", "usage-open", "nav-ledger"].forEach(function (id) {
            if (node(id)) node(id).hidden = board.enabled;
        });
        if (node("nav-board")) {
            node("nav-board").hidden = !board.enabled;
            node("nav-board").textContent = words("Projects · Board", "Projects · 看板");
        }
        var toggle = node("settings-board-toggle");
        if (toggle) {
            toggle.disabled = saving || !canManage();
            toggle.setAttribute("aria-pressed", String(board.enabled));
            toggle.classList.toggle("on", board.enabled);
            toggle.textContent = board.enabled ? words("Enabled", "已啟用") : words("Disabled", "已關閉");
        }
        if (node("settings-board-title")) node("settings-board-title").textContent = words("Enable Project Board", "啟用看板系統");
        if (node("settings-board-say")) node("settings-board-say").textContent = words(
            "Currently free. Organize work, sessions, usage and delivery under each Project. Disable to use the standard workflow; history is retained.",
            "目前免費。以 Project 項目整合派工、進度、用量與成果。關閉後使用一般流程，歷史紀錄仍保留。");
        if (node("settings-board-history")) node("settings-board-history").textContent = words("Open board history", "開啟看板與歷史紀錄");
        var projects = node("sidebar-board-projects");
        if (projects) {
            projects.hidden = !board.enabled;
            projects.replaceChildren();
            (board.projects || []).forEach(function (project) {
                var button = document.createElement("button");
                button.type = "button"; button.className = "sidebar-item board-project-shortcut";
                button.textContent = project.name + " · " + project.itemCount;
                button.addEventListener("click", function () { BoardControls.open(project.id); });
                projects.appendChild(button);
            });
        }
    },
    refresh: function () {
        if (!api || typeof api.board !== "function") return Promise.resolve(null);
        if (reading) return reading;
        this.bind();
        reading = Promise.resolve().then(function () { return api.board(); }).then(function (result) {
            BoardControls.apply(result.board);
            if (!operationError && !saving && node("settings-board-status")) node("settings-board-status").textContent = "";
            return result;
        }).catch(function (error) {
            if (node("settings-board-status")) node("settings-board-status").textContent = error.message || words("Board unavailable", "無法讀取看板設定");
            return null;
        }).finally(function () { reading = null; });
        return reading;
    },
    bind: function () {
        if (bound || !node("settings-board-toggle")) return;
        bound = true;
        node("settings-board-toggle").addEventListener("click", function () {
            if (!canManage() || saving || !api || !api.boardCommand) return;
            var command = pending || { operation: "set_enabled", enabled: !latest.enabled,
                expectedRevision: latest.revision, requestId: crypto.randomUUID() };
            pending = command;
            saving = true; operationError = false;
            node("settings-board-toggle").disabled = true;
            node("settings-board-status").textContent = words("Saving…", "儲存中…");
            Promise.resolve().then(function () { return api.boardCommand(command); }).then(function (result) {
                pending = null; BoardControls.apply(result.board);
                node("settings-board-status").textContent = words("Saved", "已儲存");
            }).catch(function (error) {
                // A network loss may follow a successful write: retry the same request.
                if (error.code && !/offline|busy|timeout|unavailable|network|connection|persistence_failed/.test(error.code)) {
                    pending = null; BoardControls.refresh();
                }
                operationError = true;
                node("settings-board-status").textContent = (error.message || "Save failed") +
                    words(" · Press again to retry.", " · 再按一次重試。");
            }).finally(function () {
                saving = false;
                node("settings-board-toggle").disabled = !canManage();
            });
        });
    }
};
