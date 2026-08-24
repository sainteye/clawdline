import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { assistantLogo, assistantName } from "../core/pixels.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { listUnknown } from "../view/waits.js";

/**
 * One small cache in front of the expensive session-info read. The status line needs those facts
 * while the card is closed, and opening the card immediately afterwards must not make the Mac
 * read the same transcript and run the same `git status` twice. A minute is fresh enough for a
 * glance; ending a turn and the card's Refresh button both ask explicitly for a newer answer.
 */
export var SessionFacts = (function () {
    var TTL = 60000;
    var cache = {};       // id -> { data, at }
    var pending = {};     // id -> Promise

    return {
        peek: function (id) { return cache[id] ? cache[id].data : null; },
        fresh: function (id) { return !!(cache[id] && Date.now() - cache[id].at < TTL); },
        drop: function (id) { if (id) delete cache[id]; },
        get: function (id, force) {
            if (!id || typeof api.info !== "function") return Promise.resolve(null);
            if (!force && this.fresh(id)) return Promise.resolve(cache[id].data);
            if (pending[id]) return pending[id];
            pending[id] = api.info(id).then(function (answer) {
                var data = (answer && answer.info) || null;
                cache[id] = { data: data, at: Date.now() };
                delete pending[id];
                return data;
            }).catch(function (e) {
                delete pending[id];
                throw e;
            });
            return pending[id];
        }
    };
})();

/**
 * The persistent status line under the open transcript. This is the compact reading of the
 * Session info card: model, total token usage and cost, working-tree summary, and the plan
 * windows. The whole row opens the card, just as clicking a terminal status line asks for the
 * detail behind a number.
 *
 * It used to stand itself down on a phone, back when the phone breakpoint hid the row: an
 * answer nobody could see was a read of a transcript and a `git status` on the Mac for nothing.
 * The row is on every screen now, so the reading is taken on every screen — still at most once
 * a minute, and still shared with the card through `SessionFacts`.
 */
export var StatusLine = (function () {
    var forId = null;
    var data = null;
    var ticket = 0;
    var nextAt = 0;
    var stateSeen = "";

    function compact(n) {
        if (typeof n !== "number") return T.webInfoUnknown;
        if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 1 : 2) + "M";
        if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e4 ? 0 : 1) + "K";
        return String(n);
    }
    function dollars(x) { return x < 0.01 ? "<$0.01" : "$" + x.toFixed(2); }

    function modelName(d) {
        var s = d.session || {}, u = d.usage || {};
        var current = s.model || u.model || "";
        var row = (d.models || []).filter(function (m) {
            return current && (current === m.id || current.indexOf(m.id) === 0);
        })[0];
        return row ? row.name : current;
    }

    function marks(f) {
        var rows = [["staged", "+", f.staged], ["unstaged", "*", f.unstaged],
                    ["untracked", "?", f.untracked], ["conflict", "!", f.conflict]]
            .filter(function (m) { return m[2]; });
        if (!rows.length) return '<span class="mark" data-k="clean">✓</span>';
        return rows.map(function (m) {
            return '<span class="mark" data-k="' + m[0] + '">' + m[1] + m[2] + "</span>";
        }).join("");
    }

    /** The left half: what this session is on and what it has spent. Opens the Session info card. */
    function identityHTML(d) {
        var s = d.session || {}, u = d.usage;
        var model = modelName(d) || assistantName(s.assistant);
        var out = '<span class="item model">' + assistantLogo(s.assistant) +
            '<span class="word">' + esc(model) + "</span></span>";

        if (u && typeof u.total === "number") {
            // The unit is its own element so a phone can drop it and keep the number — see the
            // rule at the phone breakpoint. The title says it in full either way.
            out += '<span class="item usage" title="' + esc(u.total.toLocaleString()) + " " +
                esc(T.webInfoTokens) + '">' + esc(compact(u.total)) +
                '<span class="unit"> ' + esc(T.webInfoTokens) + "</span></span>";
            if (typeof u.costUsd === "number") {
                out += '<span class="item cost">' + esc(dollars(u.costUsd)) + "</span>";
            }
        }
        return out;
    }

    /** The working tree. Its own button now, and what it opens is the list it is counting. */
    function filesHTML(f) {
        var branch = f.branch || (f.head ? f.head.slice(0, 7) : "");
        return (branch ? '<span class="branch">⎇ ' + esc(branch) + "</span>" : "") + marks(f);
    }

    function limitsHTML(windows) {
        return windows.map(function (w) {
            var pct = typeof w.usedPercent === "number"
                ? Math.max(0, Math.min(100, Math.round(w.usedPercent))) : null;
            var level = pct === null ? "" : (pct >= 85 ? "bad" : (pct >= 60 ? "warn" : "ok"));
            return '<span class="limit" data-level="' + level + '">' + esc(w.name) + " " +
                '<b>' + (pct === null ? esc(T.webInfoUnknown) : pct + "%") + "</b></span>";
        }).join("");
    }

    /** The two elements beside the button, drawn together because they empty together. */
    function drawRest(d) {
        var files = els["status-line-files"], limits = els["status-line-limits"];
        var f = d && d.files;
        files.hidden = !f;
        if (f) {
            files.innerHTML = filesHTML(f);
            files.title = T.webSessionGit;
            files.setAttribute("aria-label", T.webSessionGit);
        }
        var windows = ((d && d.limits) || {}).windows || [];
        limits.innerHTML = windows.length ? limitsHTML(windows) : "";
    }

    function draw() {
        var button = els["status-line-open"];
        button.title = T.webSessionInfo + " (⌘I)";
        button.setAttribute("aria-label", T.webSessionInfo);
        button.disabled = !forId;
        if (!forId) {
            // Same rule as the pane above it: nobody has said yet is not the same as nobody is
            // open, and only one of the two is worth a sentence. See `listUnknown`.
            button.innerHTML = listUnknown() ? ""
                : '<span class="empty">' + esc(T.webPickSession) + "</span>";
            drawRest(null);
            return;
        }
        if (data) {
            button.innerHTML = identityHTML(data);
            drawRest(data);
            return;
        }
        var s = byId(forId) || {};
        button.innerHTML = '<span class="item model">' + assistantLogo(s.assistant) +
            '<span class="word">' + esc(assistantName(s.assistant)) + '</span></span>' +
            '<span class="item empty">' + esc(T.webLoading) + "</span>";
        drawRest(null);
    }

    function load(force) {
        if (!forId) return;
        var id = forId, mine = ++ticket;
        nextAt = Date.now() + 60000;
        SessionFacts.get(id, force).then(function (facts) {
            if (mine !== ticket || forId !== id) return;
            data = facts;
            draw();
        }).catch(function () {
            if (mine !== ticket || forId !== id) return;
            // Keep the last good reading. If there was none, the basic session identity remains.
            draw();
        });
    }

    return {
        follow: function () {
            var id = S.openId;
            var s = id ? byId(id) : null;
            var state = s ? s.state : "";
            if (id !== forId) {
                ticket += 1;
                forId = id;
                data = id ? SessionFacts.peek(id) : null;
                stateSeen = state;
                nextAt = 0;
                draw();
                if (id) load(false);
                return;
            }
            // The completed turn is when totals and limits have most likely changed.
            var endedTurn = stateSeen === "working" && state !== "working";
            stateSeen = state;
            if (endedTurn) load(true);
            else if (id && Date.now() >= nextAt) load(false);
        },
        refresh: function (force) {
            if (!forId) return;
            if (force || Date.now() >= nextAt) load(!!force);
        },
        receive: function (id, facts) {
            if (id !== forId || !facts) return;
            data = facts;
            nextAt = Date.now() + 60000;
            draw();
        }
    };
})();
