import { phone } from "../core/env.js";
import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";
import { els } from "../core/dom.js";
import { api } from "../net/api.js";
import { SessionActions } from "./detail-actions.js";
import { SessionSelection } from "../session/selection.js";
import { callSessionUI } from "../session/ui.js";

/**
 * A read-only view of the open session's repository, occupying the transcript's space.
 *
 * It owns no cache beyond the time it is visible. Opening and refreshing both ask Git at that
 * moment, and a ticket makes an answer for the previous session harmless if the reader moves on
 * while it is in flight.
 */
export var GitPanel = (function () {
    var forSelection = null;
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
        var selected = forSelection;
        if (!selected) return;
        var mine = ++ticket;
        var effect = SessionSelection.beginEffect("git-panel", { identity: selected });
        snapshot = null; error = null; loading = true;
        render();
        api.git(selected.route).then(function (data) {
            if (mine !== ticket || forSelection !== selected ||
                !SessionSelection.effectIsCurrent(effect)) return;
            snapshot = data.git || { files: [], clean: true };
            loading = false;
            render();
        }).catch(function (e) {
            if (mine !== ticket || forSelection !== selected ||
                !SessionSelection.effectIsCurrent(effect)) return;
            loading = false;
            error = e && e.code === "not_a_repo" ? T.webGitNotRepo : T.webGitFailed;
            render();
        }).then(function () { SessionSelection.finishEffect(effect); });
    }

    return {
        open: function () {
            var selected = SessionSelection.snapshot().open;
            if (!selected) return;
            SessionActions.close();
            callSessionUI("closeShellPanel", false);
            callSessionUI("closeTerminal", false);
            forSelection = selected;
            els["git-panel"].hidden = false;
            els["pane-detail"].dataset.panel = "git";
            load();
            els["git-close"].focus({ preventScroll: true });
        },

        close: function (restore) {
            if (els["git-panel"].hidden) return;
            ticket += 1;
            forSelection = null; snapshot = null; loading = false; error = null;
            els["git-panel"].hidden = true;
            delete els["pane-detail"].dataset.panel;
            if (restore && !els["detail-actions-trigger"].disabled) {
                els["detail-actions-trigger"].focus({ preventScroll: true });
            }
        },

        refresh: function () { if (forSelection) load(); },
        follow: function () { this.close(false); }
    };
})();
