import { phone } from "../core/env.js";
import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { api } from "../net/api.js";
import { SessionActions } from "./detail-actions.js";
// The two panels share the transcript's space and the attribute that says so, so each has to put
// the other down. They import each other, which is fine here and only here: neither touches the
// other while its own module is being evaluated — only later, inside `open`.
import { ShellPanel } from "./shell-panel.js";
import { Terminal } from "../view/terminal.js";

/**
 * A read-only view of the open session's repository, occupying the transcript's space.
 *
 * It owns no cache beyond the time it is visible. Opening and refreshing both ask Git at that
 * moment, and a ticket makes an answer for the previous session harmless if the reader moves on
 * while it is in flight.
 */
export var GitPanel = (function () {
    var forId = null;
    var snapshot = null;
    var loading = false;
    var error = null;
    var ticket = 0;

    function shortened(path) {
        path = String(path || "");
        var limit = phone() ? 34 : 72;
        if (path.length <= limit) return path;
        var tail = Math.floor(limit * 0.65);
        return path.slice(0, limit - tail - 1) + "…" + path.slice(-tail);
    }

    function mark(file) {
        if (file.kind === "conflict") return { text: "!", label: T.webGitConflict };
        if (file.kind === "untracked") return { text: "?", label: T.webGitUntracked };
        var text = "", labels = [];
        if (file.staged) { text += "+"; labels.push(T.webGitStaged); }
        if (file.unstaged) { text += "*"; labels.push(T.webGitUnstaged); }
        return { text: text || "·", label: labels.join(", ") };
    }

    function row(file) {
        var state = mark(file);
        var title = file.from ? String(file.from) + " → " + String(file.path) : String(file.path);
        var hasStats = typeof file.additions === "number" && typeof file.deletions === "number";
        var stats = hasStats
            ? '<span class="stats"><span class="add">+' + esc(file.additions) +
              '</span> <span class="del">−' + esc(file.deletions) + "</span></span>"
            : '<span class="stats"></span>';
        return '<li class="git-file" data-kind="' + esc(file.kind || "modified") + '">' +
            '<span class="mark" aria-label="' + esc(state.label) + '" title="' +
                esc(state.label) + '">' + esc(state.text) + "</span>" +
            '<span class="path" title="' + esc(title) + '">' + esc(shortened(file.path)) +
                "</span>" + stats + "</li>";
    }

    function render() {
        if (loading) {
            els["git-body"].innerHTML = '<div class="git-note" role="status">' +
                esc(T.webLoading) + "</div>";
            return;
        }
        if (error) {
            els["git-body"].innerHTML = '<div class="git-note err" role="alert">' +
                esc(error) + "</div>";
            return;
        }
        var git = snapshot || {};
        var branch = "⎇ " + (git.branch || String(git.head || "").slice(0, 8)) +
            " ↑" + (git.ahead || 0) + " ↓" + (git.behind || 0);
        var files = git.files || [];
        els["git-body"].innerHTML = '<div class="git-branch">' + esc(branch) + "</div>" +
            (git.clean || !files.length
                ? '<div class="git-note">' + esc(T.webGitClean) + "</div>"
                : '<ul class="git-files">' + files.map(row).join("") + "</ul>");
    }

    function load() {
        var id = forId;
        if (!id) return;
        var mine = ++ticket;
        snapshot = null; error = null; loading = true;
        render();
        api.git(id).then(function (data) {
            if (mine !== ticket || forId !== id) return;
            snapshot = data.git || { files: [], clean: true };
            loading = false;
            render();
        }).catch(function (e) {
            if (mine !== ticket || forId !== id) return;
            loading = false;
            error = e && e.code === "not_a_repo" ? T.webGitNotRepo : T.webGitFailed;
            render();
        });
    }

    return {
        open: function () {
            if (!S.openId) return;
            SessionActions.close();
            ShellPanel.close(false);
            Terminal.close(false);
            forId = S.openId;
            els["git-panel"].hidden = false;
            els["pane-detail"].dataset.panel = "git";
            load();
            els["git-close"].focus({ preventScroll: true });
        },

        close: function (restore) {
            if (els["git-panel"].hidden) return;
            ticket += 1;
            forId = null; snapshot = null; loading = false; error = null;
            els["git-panel"].hidden = true;
            delete els["pane-detail"].dataset.panel;
            if (restore && !els["detail-actions-trigger"].disabled) {
                els["detail-actions-trigger"].focus({ preventScroll: true });
            }
        },

        refresh: function () { if (forId) load(); },
        follow: function () { this.close(false); }
    };
})();
