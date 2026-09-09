import { api } from "../net/api.js";
import { Pages } from "../core/pages.js";

function words(en, zh) {
    return /^zh/i.test(document.documentElement.lang || navigator.language || "") ? zh : en;
}
function node(id) { return document.getElementById(id); }
var latest = null, reading = null, pending = null, bound = false, saving = false, operationError = false;
function canManage() { return !!(latest && latest.viewer && latest.viewer.canManage === true); }

export var BoardControls = {
    onChange: function () {},
    escape: function () { Pages.goHome(); },
    open: function () { Pages.go("board"); },
    apply: function (board) {
        if (!board || typeof board.enabled !== "boolean") return;
        if (latest && board.revision < latest.revision) return;
        if (!board.viewer && latest) board = Object.assign({}, board, { viewer: latest.viewer });
        latest = board;
        BoardControls.onChange(board.enabled);
        if (node("projects-lede")) node("projects-lede").textContent = board.enabled
            ? words("Choose a project to see its work, progress and results.", "選擇專案，了解正在進行的工作與已落地的成果。")
            : words("Directories an assistant has actually been run in, and that are still there.", "assistant 真的跑過、而且還在的目錄。");
        document.documentElement.dataset.boardMode = board.enabled ? "board" : "standard";
        ["usage-open", "nav-ledger"].forEach(function (id) {
            if (node(id)) node(id).hidden = board.enabled;
        });
        if (node("nav-board")) {
            node("nav-board").hidden = true;
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
        if (node("settings-board-history")) node("settings-board-history").textContent = words("Open projects", "開啟專案");
        var provider = board.viewer && board.viewer.narrativeProvider;
        var providerName = provider === "codex" ? "OpenAI / Codex" : provider === "claude" ? "Anthropic / Claude" : "";
        var allowed = !!provider && board.narrativeConsent === provider;
        if (node("settings-board-ai-title")) node("settings-board-ai-title").textContent = words("AI reading summaries", "AI 閱讀摘要");
        if (node("settings-board-ai-say")) node("settings-board-ai-say").textContent = words(
            "Separate opt-in. Send stored Board titles, descriptions and documented outcomes to ",
            "獨立同意設定。將已儲存的看板標題、描述與成果文字送至 ") + providerName + words(
            " to summarize in your Clawdline language. Uses the configured naming model and its quota. No transcript, credential-file or attachment reading. Original text stays available; Board OFF stops generation.",
            "，以 Clawdline 設定語言整理；使用已設定的命名模型與其額度。不讀取完整對話、憑證檔或附件；原文保留，關閉看板即停止生成。");
        if (node("settings-board-ai-toggle")) {
            var ai = node("settings-board-ai-toggle");
            ai.disabled = saving || !canManage() || !providerName;
            ai.setAttribute("aria-pressed", String(allowed));
            ai.textContent = allowed ? words("Stop AI sharing", "停止 AI 外送整理") : words("Allow sharing with ", "同意送至 ") + providerName;
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
        if (node("settings-board-ai-toggle")) node("settings-board-ai-toggle").addEventListener("click", function () {
            if (!canManage() || saving || !api || !api.boardCommand) return;
            var provider = latest.viewer && latest.viewer.narrativeProvider;
            if (!["codex", "claude"].includes(provider)) return;
            var command = pending || { operation: "set_ai_consent", provider: provider,
                enabled: latest.narrativeConsent !== provider, policy: "board-reading-v1",
                expectedRevision: latest.revision, requestId: crypto.randomUUID() };
            if (command.operation !== "set_ai_consent") return;
            pending = command; saving = true;
            BoardControls.apply(latest);
            Promise.resolve().then(function () { return api.boardCommand(command); }).then(function (result) {
                pending = null; operationError = false; BoardControls.apply(result.board);
                node("settings-board-status").textContent = words("Saved", "已儲存");
            }).catch(function (error) {
                operationError = true;
                if (error.code && !/offline|busy|timeout|unavailable|network|connection|persistence_failed/.test(error.code)) pending = null;
                node("settings-board-status").textContent = error.message || words("Save failed", "儲存失敗");
            }).finally(function () { saving = false; BoardControls.apply(latest); });
        });
        node("settings-board-toggle").addEventListener("click", function () {
            if (!canManage() || saving || !api || !api.boardCommand) return;
            var command = pending || { operation: "set_enabled", enabled: !latest.enabled,
                expectedRevision: latest.revision, requestId: crypto.randomUUID() };
            if (command.operation !== "set_enabled") return;
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
