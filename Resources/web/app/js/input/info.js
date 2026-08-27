import { atMac } from "../core/env.js";
import { esc } from "../core/esc.js";
import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { shortPath, toast } from "../core/util.js";
import { assistantLogo } from "../core/pixels.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { GitPanel } from "./git-panel.js";
import { SessionFacts, StatusLine } from "./status-line.js";

/**
 * The Session info card — the status line at the bottom of a Claude Code terminal, for somebody
 * who is not at that terminal: what this session is and what model it is on, what it has
 * spent, what is left of the plan's window, how much has changed on disk, and whether the last
 * deploy went out.
 *
 * It shares the status line's short-lived answer. A refresh keeps the old card on screen until
 * the new one arrives — a card that blanks itself to say "loading" has thrown away the one thing
 * somebody was comparing against. A plan window nobody reported is drawn as *unknown* and never
 * as 0%: a full window shown as an empty one is the one wrong answer that changes what somebody
 * does next.
 */
export var Info = (function () {
    var STATES = { ok: "webLinkOk", fail: "webLinkFail", down: "webLinkDown", running: "webLinkRunning" };
    // Where the `?` beside an unknown plan window goes: the page of the manual that says why it
    // is unknown and what to install so that it is not. A window that cannot be read is the one
    // fact on this card somebody can do something about, so it is the one with a door.
    var HELP_LIMITS = "https://github.com/sainteye/clawdline/blob/main/docs/remote.md#the-plan-windows-say-unknown";

    var forId = null;    // whose card this is
    var data = null;     // as the Mac sent it; null until an answer has arrived
    var loading = false;
    var ticket = 0;      // the answer that is still wanted, so a stale one can be dropped
    var drawn = false;   // the sections have risen once; a redraw should not make them rise again
    var pending = null;  // the word sent after `/model`, until the transcript names that model
    var permissionPending = null; // the mode requested, until a fresh screen capture agrees
    var busy = false;    // a model command or permission key sequence on its way to the Mac
    var stateSeen = "";  // the session's state at the last draw, so a change redraws the buttons
    var confirming = 0;  // the timer reading back, waiting for a sent `/model` to turn up
    var permissionConfirming = 0;

    function say(w) { els["info-say"].textContent = w || ""; els["info-say"].hidden = !w; }
    function said(w, calm) {
        els["info-said"].textContent = w || "";
        els["info-said"].className = "said" + (calm ? " calm" : "");
    }
    /// What the Mac refused with. `busy` is the third of its kind on this page — see
    /// `webVoiceBusy` and `webCommandBusy` — and it is the one thing here that fixes itself: this
    /// card is one of three routes sharing a limit, and the answer is to ask again in a moment.
    /// Drawn as "could not read this session's info" it reads as a session gone wrong instead.
    function why(e) {
        var code = e && e.code;
        if (code === "offline") return e.message;   // already this page's own sentence
        if (code === "busy") return T.webInfoBusy;
        return T.webInfoFailed;
    }

    /** `1d 2h`, `2h 14m`, `14m` — the status line's own spelling, which needs no translating. */
    function span(seconds) {
        var s = Math.max(0, Math.floor(seconds));
        var d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
        if (d) return d + "d " + h + "h";
        if (h) return h + "h " + m + "m";
        return m + "m";
    }

    /** When a window comes back: a clock if it is today, else the day, in the reader's locale. */
    function resetWhen(unix) {
        if (!unix) return "";
        var left = unix - Date.now() / 1000;
        if (left <= 0) return "";
        var d = new Date(unix * 1000);
        if (left < 86400) {
            var h = d.getHours(), m = d.getMinutes();
            return (h < 10 ? "0" : "") + h + ":" + (m < 10 ? "0" : "") + m;
        }
        try { return d.toLocaleDateString(undefined, { weekday: "short" }); } catch (e) { return d.toDateString().slice(0, 3); }
    }

    /** A moment in the past: a clock if it was within the day, else the day, in the reader's locale. */
    function clockOf(unix) {
        if (!unix) return "";
        var d = new Date(unix * 1000);
        if (Date.now() / 1000 - unix < 86400) {
            var h = d.getHours(), m = d.getMinutes();
            return (h < 10 ? "0" : "") + h + ":" + (m < 10 ? "0" : "") + m;
        }
        try { return d.toLocaleDateString(undefined, { weekday: "short" }); } catch (e) { return d.toDateString().slice(0, 3); }
    }

    function count(x) { return typeof x === "number" ? esc(x.toLocaleString()) : esc(T.webInfoUnknown); }
    /** `6.59M`, `80.1K`, `318` — the size of a number at a glance; the exact one is in the title. */
    function compact(n) {
        if (typeof n !== "number") return T.webInfoUnknown;
        if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 1 : 2) + "M";
        if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e4 ? 0 : 1) + "K";
        return String(n);
    }
    function dollars(x) { return x < 0.01 ? "<$0.01" : "$" + x.toFixed(2); }
    function openable(url) { return /^https?:\/\//i.test(url || ""); }
    function isFile(url) { return /^file:\/\//i.test(url || ""); }
    function pathOf(url) {
        var raw = String(url).replace(/^file:\/\/(localhost)?/i, "");
        try { return decodeURIComponent(raw); } catch (e) { return raw; }
    }

    function sec(i, title, aside, body) {
        return '<section class="sec" style="--i:' + i + '"><h3><span>' + esc(title) + "</span>" +
            (aside ? '<span class="aside">' + aside + "</span>" : "") + "</h3>" + body + "</section>";
    }
    function note(text) { return '<p class="note">' + esc(text) + "</p>"; }

    function session() { return forId ? byId(forId) : null; }
    /** Only an idle session takes a `/model`: typed into a working one it interrupts the turn
     *  with a question on the Mac, and typed into one that is asking, it is the answer. */
    function canSwitch() { var s = session(); return !!(s && S.write && s.state === "idle" && !busy); }
    function canSwitchPermission() { return !!(session() && S.write && !busy); }
    /** The row the session is on. By prefix, so `claude-haiku-4-5-20251001` finds `claude-haiku-4-5`. */
    function onModel(current, m) { return !!current && (current === m.id || current.indexOf(m.id) === 0); }

    function hero(s, u) {
        var model = s.model || (u && u.model) || "";
        // The model used to be the headline, which meant the one identity the Session list was
        // built around disappeared as soon as this card opened. Keep old servers readable by
        // falling back to their model, but a current payload gives the complete, unabridged
        // Session title this prominent place and keeps the model beside the assistant.
        var title = s.title || model || T.webInfoUnknown;
        var modelMeta = s.title && model
            ? '<span class="dot">·</span><span class="model-name">' + esc(model) + "</span>" : "";
        var meta = [];
        if (s.cwd) meta.push('<span title="' + esc(T.webInfoDirectory) + '">' + esc(shortPath(s.cwd)) + "</span>");
        if (s.sessionId) {
            meta.push('<button type="button" class="sid" data-copy="' + esc(s.sessionId) + '" title="' +
                esc(T.webInfoSessionId + ": " + s.sessionId) + '">' + esc(String(s.sessionId).slice(0, 8)) + "</button>");
        }
        if (typeof s.seconds === "number") {
            meta.push('<span title="' + esc(T.webInfoRunningFor) + '">' + esc(span(s.seconds)) + "</span>");
        }
        return '<div class="hero">' +
            '<div class="who">' + assistantLogo(s.assistant) + '<span class="assistant-name">' +
                esc(s.assistant || T.webInfoUnknown) + "</span>" + modelMeta + "</div>" +
            '<div class="session-title">' + esc(title) + "</div>" +
            '<div class="meta">' + meta.join('<span class="dot">·</span>') + "</div>" +
            "</div>";
    }

    function modelsHTML(models, current) {
        var can = canSwitch();
        var chips = models.map(function (m) {
            var on = onModel(current, m);
            var wait = !on && pending === m.command;
            return '<button type="button" class="m" data-model="' + esc(m.command) + '" data-name="' + esc(m.name) + '"' +
                (on ? ' data-on="1" aria-current="true"' : "") + (wait ? ' data-pending="1"' : "") +
                (can && !on ? "" : " disabled") + ">" + esc(m.name) + "</button>";
        }).join("");
        // A word typed rather than picked has no row of its own, so it is drawn as one while it
        // is on its way.
        if (pending && !models.some(function (m) { return m.command === pending; })) {
            chips += '<button type="button" class="m" data-pending="1" disabled>' + esc(pending) + "</button>";
        }
        var other = '<form class="other" data-other="1">' +
            '<input type="text" name="model" autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false" ' +
            'placeholder="' + esc(T.webInfoModelOther) + '" aria-label="' + esc(T.webInfoModelOther) + '"' + (can ? "" : " disabled") + ">" +
            '<button type="submit" class="chip"' + (can ? "" : " disabled") + ">" + esc(T.webSend) + "</button></form>";
        return '<div class="models">' + chips + other + "</div>" + (can ? "" : note(T.webInfoModelBusy));
    }

    function permissionName(mode) {
        var names = {
            auto: "webInfoPermissionAuto", manual: "webInfoPermissionManual",
            acceptEdits: "webInfoPermissionAcceptEdits", plan: "webInfoPermissionPlan"
        };
        return names[mode] ? T[names[mode]] : mode;
    }

    /** The server's option order is Claude Code's cycle order, so the distance is both a UI
     *  calculation and the exact number of Back-Tabs to send. Unknown has no index on purpose. */
    function permissionSteps(permission, target) {
        var options = permission && permission.options || [];
        var start = options.indexOf(permission && permission.current);
        var end = options.indexOf(target);
        if (start < 0 || end < 0 || !options.length) return null;
        return (end - start + options.length) % options.length;
    }

    function permissionHTML(permission) {
        if (!permission || permission.current === "unknown") return note(T.webInfoPermissionUnreadable);
        var can = canSwitchPermission();
        var chips = (permission.options || []).map(function (mode) {
            var on = permission.current === mode;
            var wait = !on && permissionPending === mode;
            return '<button type="button" class="m" data-permission="' + esc(mode) + '"' +
                (on ? ' data-on="1" aria-current="true"' : "") + (wait ? ' data-pending="1"' : "") +
                (can && !on ? "" : " disabled") + ">" + esc(permissionName(mode)) + "</button>";
        }).join("");
        return '<div class="models">' + chips + "</div>" + (can ? "" : note(T.webInfoPermissionBusy));
    }

    function usageHTML(u) {
        if (!u) return note(T.webInfoNoUsage);
        var cells = [[u.input, T.webInfoInput], [u.output, T.webInfoOutput],
                     [u.cacheRead, T.webInfoCacheRead], [u.cacheWrite, T.webInfoCacheWrite]];
        return '<div class="big"><span class="n" title="' + count(u.total) + '">' + esc(compact(u.total)) + "</span>" +
            '<span class="unit">' + esc(T.webInfoTokens) + "</span>" +
            // Only where a price is known. Codex bills against a plan rather than per token, and
            // the Mac says nothing rather than a made-up number — so this says nothing too.
            (typeof u.costUsd === "number" ? '<span class="cost">' + esc(dollars(u.costUsd)) + "</span>" : "") +
            "</div>" +
            '<div class="grid">' + cells.map(function (c) {
                return '<div class="cell"><b title="' + count(c[0]) + '">' + esc(compact(c[0])) + "</b><i>" + esc(c[1]) + "</i></div>";
            }).join("") + "</div>";
    }

    function limitsHTML(l, assistant) {
        var windows = l.windows || [];
        // Nothing known is said as such, never as 0% — and for Claude, why: its transcript only
        // records a window once it has been spent.
        if (!windows.length) {
            if (assistant !== "claude") return note(T.webInfoUnknown);
            return '<p class="note">' + esc(T.webInfoLimitsClaude) +
                ' <a class="why" href="' + HELP_LIMITS + '" target="_blank" rel="noopener noreferrer" title="' +
                esc(T.webInfoWhyUnknown) + '" aria-label="' + esc(T.webInfoWhyUnknown) + '">?</a></p>';
        }
        return windows.map(function (w) {
            var pct = typeof w.usedPercent === "number" ? Math.max(0, Math.min(100, Math.round(w.usedPercent))) : null;
            var level = pct === null ? "" : (pct >= 85 ? "bad" : (pct >= 60 ? "warn" : "ok"));
            var tail = [];
            if (w.hit) tail.push(esc(T.webInfoLimitHit));
            var when = resetWhen(w.resetsAt);
            if (when) tail.push(esc(fill(T.webInfoResets, { when: when })));
            return '<div class="win" data-level="' + level + '"><span class="wn">' + esc(w.name) + "</span>" +
                '<span class="bar"><i data-w="' + (pct === null ? 0 : pct) + '%"></i></span>' +
                '<span class="pct">' + (pct === null ? esc(T.webInfoUnknown) : pct + "%") + "</span>" +
                (tail.length ? '<span class="when">' + tail.join(" · ") + "</span>" : "") + "</div>";
        }).join("");
    }

    function branchOf(f) {
        if (!f) return "";
        var b = f.branch || (f.head ? f.head.slice(0, 7) : "");
        if (!b) return "";
        var out = "⎇ " + esc(b);
        if (f.ahead) out += ' <span class="ab">↑' + f.ahead + "</span>";
        if (f.behind) out += ' <span class="ab">↓' + f.behind + "</span>";
        return out;
    }

    function filesHTML(f) {
        if (!f) return note(T.webInfoNotRepo);
        var marks = [["staged", "+", f.staged, T.webInfoStaged], ["unstaged", "*", f.unstaged, T.webInfoUnstaged],
                     ["untracked", "?", f.untracked, T.webInfoUntracked], ["conflict", "!", f.conflict, T.webInfoConflict]]
            .filter(function (m) { return m[2]; });
        if (!marks.length) return '<div class="marks"><span class="mk" data-k="clean"><b>✓</b>' + esc(T.webInfoClean) + "</span></div>";
        return '<div class="marks">' + marks.map(function (m) {
            return '<span class="mk" data-k="' + m[0] + '"><b>' + m[1] + m[2] + "</b>" + esc(m[3]) + "</span>";
        }).join("") + "</div>";
    }

    function linksHTML(links) {
        if (!links.length) return note(T.webLinksEmpty);
        return links.map(function (link) {
            var word = STATES[link.state] ? T[STATES[link.state]] : "";
            var url = String(link.url || "");
            var file = isFile(url);
            var far = link.local && !atMac();
            var detail = link.why ? String(link.why)
                : (file ? T.webLinksFile : (far ? T.webLinksLocal : ""));
            var where = file ? shortPath(pathOf(url)) : url.replace(/^https?:\/\//i, "");
            var inner = '<span class="dot"></span><span class="lbl">' + esc(link.label || link.kind) + "</span>" +
                (word ? '<span class="st">' + esc(word) + "</span>" : "") +
                '<span class="host" title="' + esc(file ? pathOf(url) : url) + '">' + esc(where) + "</span>";
            // A `javascript:` in an `href` is script running on this page with this page's
            // cookie, so anything not plainly http(s) is drawn as text.
            var row = openable(url)
                ? '<a class="dep" data-state="' + esc(link.state || "") + '" href="' + esc(url) + '" target="_blank" rel="noopener noreferrer">' + inner + "</a>"
                : '<div class="dep" data-state="' + esc(link.state || "") + '">' + inner + "</div>";
            return '<div class="dep-row">' + row +
                (detail ? '<p class="dep-note">' + esc(detail) + "</p>" : "") + "</div>";
        }).join("");
    }

    function html(d) {
        var s = d.session || {}, u = d.usage, l = d.limits || {}, f = d.files,
            links = d.links || d.deploy || [], models = d.models || [], permission = d.permission;
        // The model is whatever the transcript last named — a reply, or a `/model` nobody has
        // replied to yet, whichever of the two is newer. So a session that switched mid-way shows
        // what it is on now rather than what it began on, a session that has only ever been
        // switched shows that rather than nothing, and a `/model` this card sent stops being
        // pending the moment the record agrees with it.
        var current = s.model || (u && u.model) || "";
        if (pending && models.some(function (m) { return m.command === pending && onModel(current, m); })) pending = null;
        if (permissionPending && permission && permission.current === permissionPending) permissionPending = null;
        var out = hero(s, u), i = 0;
        // Read-only pairings get no buttons rather than dead ones: there is nothing they could do.
        if (models.length && S.write) out += sec(++i, T.webInfoSwitchModel, "", modelsHTML(models, current));
        if (permission && S.write) out += sec(++i, T.webInfoSwitchPermission, "", permissionHTML(permission));
        out += sec(++i, T.webInfoUsage, "", usageHTML(u));
        // When the windows were last known — the status line writes them down every few seconds
        // while a Claude Code session is open, and a stale reading should look its age.
        var known = (l.windows || []).length && l.at ? esc(fill(T.webInfoAsOf, { when: clockOf(l.at) })) : "";
        out += sec(++i, T.webInfoLimits, known, limitsHTML(l, s.assistant));
        out += sec(++i, T.webInfoFiles, branchOf(f), filesHTML(f));
        out += sec(++i, T.webLinks, "", linksHTML(links));
        return out;
    }

    function draw() {
        if (els.info.hidden) return;
        var s = session();
        stateSeen = s ? s.state : "";
        say(loading && !data ? T.webLoading : "");
        // A redraw under somebody's fingers keeps what they had typed and where the caret was.
        var box = els["info-body"];
        var typed = box.querySelector(".other input");
        var kept = typed ? typed.value : "";
        var focused = !!typed && document.activeElement === typed;
        var again = drawn;
        box.classList.toggle("again", again);
        box.innerHTML = data ? html(data) : "";
        if (data) drawn = true;
        els["info-refresh"].disabled = loading;
        typed = box.querySelector(".other input");
        if (typed && kept) typed.value = kept;
        if (typed && focused && !typed.disabled) typed.focus({ preventScroll: true });
        // The bars grow from nothing to their number the first time: laid out once at nothing
        // (the read of `offsetWidth` is what forces that), then given the number, so the
        // transition has somewhere to start. Not on a frame callback — a tab in the background
        // gets none, and a card opened there would show every window as empty. A redraw sets
        // them straight away: a bar that shrank to nothing and grew back every time the session
        // changed state would be a nervous tic, not a picture.
        var bars = box.querySelectorAll(".bar i");
        if (!again && bars.length) void box.offsetWidth;
        for (var k = 0; k < bars.length; k++) bars[k].style.setProperty("--w", bars[k].dataset.w);
    }

    function load(id, force) {
        var mine = ++ticket;
        loading = true;
        said("");
        draw();
        SessionFacts.get(id, !!force).then(function (facts) {
            if (mine !== ticket) return;      // closed, or opened again on another session
            data = facts;
            loading = false;
            StatusLine.receive(id, facts);
            draw();
        }).catch(function (e) {
            if (mine !== ticket) return;
            loading = false;
            said(why(e));
            draw();
        });
    }

    /**
     * Read back until a sent `/model` turns up, then stop.
     *
     * The send is not the confirmation. What the Mac reports is that the line went into the
     * terminal; what Claude Code did with it is written to the transcript a moment later, and the
     * card is the thing that reads transcripts. So it reads again, a beat apart, a few times over
     * — and the chip stops being pending the moment an answer names the new model, because that
     * is `html`'s rule and not a second one invented here.
     *
     * **Quietly**, which is the whole reason this is not `load`. Nobody asked for a refresh: it
     * must not blank the card, must not grey out the refresh button, and must not wipe the line
     * saying what was sent — all of which a real load does, and all of which would read as the
     * card doing something on its own three times in a row.
     *
     * Nothing here reports a failure. A `/model` that never lands leaves the chip pending, which
     * is exactly what an outstanding request looks like, and the refresh button is right there.
     */
    function readBack(id, tries) {
        clearTimeout(confirming);
        if (tries <= 0) return;
        confirming = setTimeout(function () {
            if (forId !== id || els.info.hidden || !pending) return;
            SessionFacts.drop(id);
            SessionFacts.get(id, true).then(function (facts) {
                if (forId !== id || els.info.hidden || !pending || !facts) return;
                data = facts;
                StatusLine.receive(id, facts);
                draw();
                if (pending) readBack(id, tries - 1);
                else said("");
            }).catch(function () {});
        }, 800);
    }

    function readPermissionBack(id, tries) {
        clearTimeout(permissionConfirming);
        if (tries <= 0) return;
        permissionConfirming = setTimeout(function () {
            if (forId !== id || els.info.hidden || !permissionPending) return;
            SessionFacts.drop(id);
            SessionFacts.get(id, true).then(function (facts) {
                if (forId !== id || els.info.hidden || !permissionPending || !facts) return;
                data = facts;
                StatusLine.receive(id, facts);
                draw();
                if (permissionPending) readPermissionBack(id, tries - 1);
                else said("");
            }).catch(function () {});
        }, 500);
    }

    function pause(ms) { return new Promise(function (done) { setTimeout(done, ms); }); }

    /** Send each Back-Tab as its own idempotent write, with enough space for Claude Code to
     *  consume and repaint between them. Sending the escape sequence itself remains one atomic
     *  terminal write on the server; this delay is between complete keys, never their bytes. */
    function permissionKeys(id, count) {
        var sent = 0;
        function next() {
            return api.key(id, "shift+tab").then(function () {
                sent += 1;
                return sent < count ? pause(180).then(next) : null;
            });
        }
        return next();
    }

    return {
        open: function () {
            if (!S.openId) return;
            forId = S.openId;
            data = SessionFacts.peek(forId);
            drawn = false;
            pending = null;
            permissionPending = null;
            busy = false;
            clearTimeout(confirming);
            clearTimeout(permissionConfirming);
            said("");
            els.info.hidden = false;
            load(forId, false);
            els["info-close"].focus({ preventScroll: true });
        },

        refresh: function () {
            if (els.info.hidden || !forId || loading) return;
            load(forId, true);
        },

        close: function () {
            els.info.hidden = true;
            // An answer still on its way is no longer wanted by the card — see `ticket`. The
            // shared cache remains, because the status line is still showing this session.
            ticket += 1;
            loading = false;
            data = null;
            forId = null;
            pending = null;
            permissionPending = null;
            busy = false;
            clearTimeout(confirming);
            clearTimeout(permissionConfirming);
        },

        /** The session under the card changed, so what is in it is about something else; or
         *  its state did, and the buttons under the model depend on that. Redrawn only then: a
         *  redraw wipes a half-typed model name. */
        follow: function () {
            if (els.info.hidden) return;
            if (S.openId !== forId) { this.close(); return; }
            // The status line reads the same answer on its own clock and leaves it in the shared
            // cache. A card open beside it should not be the last to know: a `/model` typed into
            // the terminal changes what this card is about, and nobody who typed it expects to
            // have to press refresh to find that out.
            var shared = SessionFacts.peek(forId);
            if (shared && shared !== data) { data = shared; draw(); return; }
            var s = session();
            if (data && s && s.state !== stateSeen) draw();
        },

        /** `/model <word>`, typed into the session as one line. One word: the assistants take
         *  no more than one, and a second would be typed into the terminal as part of it. */
        switchTo: function (word, name) {
            var id = forId;
            word = String(word || "").trim().split(/\s+/)[0] || "";
            if (!word || !canSwitch()) return;
            busy = true;
            said("");
            draw();
            api.send(id, "/model " + word, []).then(function () {
                if (forId !== id) return;
                busy = false;
                pending = word;
                SessionFacts.drop(id);
                said(fill(T.webInfoModelSent, { model: name || word }), true);
                draw();
                readBack(id, 5);
            }).catch(function (e) {
                if (forId !== id) return;
                busy = false;
                clearTimeout(confirming);
                said((e && e.message) || T.webInfoFailed);
                draw();
            });
        },

        switchPermission: function (mode) {
            var id = forId;
            var permission = data && data.permission;
            var steps = permissionSteps(permission, mode);
            if (!steps || !canSwitchPermission()) return;
            busy = true;
            permissionPending = mode;
            said("");
            draw();
            permissionKeys(id, steps).then(function () {
                if (forId !== id) return;
                busy = false;
                SessionFacts.drop(id);
                said(fill(T.webInfoPermissionSent, { mode: permissionName(mode) }), true);
                draw();
                if (permissionPending) readPermissionBack(id, 8);
                else said("");
            }).catch(function (e) {
                if (forId !== id) return;
                busy = false;
                permissionPending = null;
                clearTimeout(permissionConfirming);
                said((e && e.message) || T.webInfoFailed);
                draw();
            });
        },

        copy: function (text) {
            if (!text || !navigator.clipboard) return;
            navigator.clipboard.writeText(text).then(function () { toast(T.webInfoCopied); }).catch(function () {});
        }
    };
})();

els.info.addEventListener("click", function () { Info.close(); });
els["info-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["info-close"].addEventListener("click", function () { Info.close(); });
els["info-refresh"].addEventListener("click", function () { Info.refresh(); });
els["info-body"].addEventListener("click", function (ev) {
    var t = ev.target;
    var chip = t.closest ? t.closest("button[data-model]") : null;
    if (chip) { if (!chip.disabled) Info.switchTo(chip.dataset.model, chip.dataset.name); return; }
    var permission = t.closest ? t.closest("button[data-permission]") : null;
    if (permission) { if (!permission.disabled) Info.switchPermission(permission.dataset.permission); return; }
    var sid = t.closest ? t.closest("button[data-copy]") : null;
    if (sid) Info.copy(sid.dataset.copy);
});
els["info-body"].addEventListener("submit", function (ev) {
    var form = ev.target;
    if (!form || !form.dataset || !form.dataset.other) return;
    ev.preventDefault();
    var input = form.querySelector("input");
    Info.switchTo(input ? input.value : "", "");
});
els["status-line-open"].addEventListener("click", function () { Info.open(); });
// The counts in this row are the sheet's own summary, so the row is where the sheet is opened
// from. It is a shorter way in than the session menu, and on a phone it is the only one that
// does not start with a menu.
els["status-line-files"].addEventListener("click", function () { GitPanel.open(); });

// A long-running turn can sit in the working state for hours, so no state transition would ask
// for a newer reading. Once a minute is intentionally slower than the terminal status line: this
// path reads a transcript and runs `git`, and the browser only needs a current glance.
setInterval(function () { StatusLine.refresh(false); }, 60000);
