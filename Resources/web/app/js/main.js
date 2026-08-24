/* --------------------------------------------------------------------------
   The entry point, and — for now — most of the page.
   The eight files below are the ones that answer to nobody: the query string,
   escaping, the words, the state, the element cache, the small helpers, the
   drawing, and the name of the API. Everything else is still here, in the order
   it was written in, waiting for the commit that gives each part its own file.
   -------------------------------------------------------------------------- */
import { MOCK, MOCK_DOOR, MOCK_FLAKY, MOCK_WRITE, atMac, hasKeyboard, params, phone, reduced } from "./core/env.js";
import { esc } from "./core/esc.js";
import { T, applyStrings, fill, words } from "./core/i18n.js";
import { S, optimisticBySession, storeBool } from "./core/state.js";
import { els } from "./core/dom.js";
import { ASK_MARK, clockOf, shortPath, tint, toast, uuid } from "./core/util.js";
import { ASSISTANT_LOGOS, assistantLogo, assistantName, bandSpin, confirmSpin, drawIcon, drawSpinner, optimisticSpinners, setBandSpin, setConfirmSpin, setLiveSpin, setOptimisticSpinners, setSpinners, setStartSpin, spinPhase, spinners, startSpin } from "./core/pixels.js";
import { api, useApi } from "./net/api.js";

/* ==========================================================================
   4. Transport
   Two implementations behind one shape: the real thing, and fixtures. Nothing
   below this section knows which one it is talking to.
   ========================================================================== */

/**
 * Whether the app on the Mac has moved on since this page was served.
 *
 * A standalone PWA is not a document somebody reloads; it is a window that sits in the app
 * switcher for days. Nothing here caches — the service worker stores nothing — so the page is
 * not stale in the browser's sense, it is simply *old*, and the browser has no reason to think
 * anything is wrong. That is how a phone came to be running a build from an hour before while
 * the Mac had been rebuilt twice, with no sign of it anywhere on screen.
 *
 * The check is the cheapest one available: `/v1/health` already answers on every connect and
 * every reconnect, and the first answer of the session is the baseline. **Anything that changes
 * when the app is rebuilt will do** — this reads `build` first and falls back to `version` and
 * the protocol number, so the day the server carries a per-build stamp this starts working
 * without another line here. (Today `version` is the string in `build.sh`, which two rebuilds of
 * the same release do not change, so today this fires only across a version bump.)
 *
 * **Never a reload of its own.** Somebody may be mid-sentence in the composer, and a page that
 * replaces itself to be newer has thrown that away to win an argument nobody was having.
 */
var Build = {
    seen: null,
    stale: false,
    rebase: false,

    stamp: function (info) {
        var parts = [info.build, info.version, info.protocol];
        var out = [];
        for (var i = 0; i < parts.length; i++) {
            if (parts[i] === null || parts[i] === undefined) continue;
            out.push(String(parts[i]));
        }
        return out.join("|");
    },

    saw: function (info) {
        if (!info || this.stale) return;
        var now = this.stamp(info);
        if (!now) return;
        if (this.seen === null || this.rebase) { this.rebase = false; this.seen = now; return; }
        if (now === this.seen) return;
        this.stale = true;
        if (els.stale) els.stale.hidden = false;
    },

    /// Told, and not reloading now.
    ///
    /// **Without this the notice cannot be got rid of.** It is correct — the page really is
    /// behind — but somebody halfway through a sentence has no way to make it go, and reloading
    /// to silence a banner is exactly what they are trying not to do. Dismissing re-bases on
    /// what the server is saying now, so it stays quiet until the *next* rebuild rather than
    /// until the end of time.
    hush: function () {
        this.stale = false;
        // The next reading becomes the new baseline. Re-basing here instead would need the
        // current answer at the moment of the click, and the click has no reason to have one.
        this.rebase = true;
        if (els.stale) els.stale.hidden = true;
    }
};

var handlers = {
    sessions: function (list, at) {
        var first = !S.arrived;
        S.sessions = list || [];
        S.at = at || 0;
        S.arrived = true;
        // Anything arriving at all is proof this browser is allowed to ask, whatever an earlier
        // request was told — a pairing finished in another tab counts.
        S.locked = false;
        // Draw the new state once. `settle(renderList)` used to run before `onSessions`, so every
        // ordinary stream frame drew the list here and then drew it again inside `onSessions`.
        // That second draw cancelled the FLIP which the first one had only just started. When a
        // skeleton is actually on screen, keep drawing it until its minimum lifetime is over and
        // let the settle callback replace it; otherwise this render is the replacement.
        var listWasWaiting = Waits.list.visible;
        onSessions();
        // Four things were held blank while nobody had said — the list, the pane beside it, that
        // pane's header and the status line under it. `onSessions` redraws all of them by opening
        // a session, but a first list with nothing to open opens none, and then nothing does.
        if (first && !S.openId) { render(); renderTranscript(); }
        Waits.list.settle(listWasWaiting ? renderList : null);
    },
    /// The whole task list, every time one of them moves. Nothing here merges, for the same
    /// reason `sessions` does not: half an update is a class of bug this page does not have.
    /// A render only if the page has been built — the first list can arrive before `boot`.
    tasks: function (list) {
        S.tasks = list || [];
        if (els.rows) render();
    },
    hello: function (info) {
        if (!info) return;
        if (typeof info.write === "boolean") S.write = info.write;
        if (info.version) S.version = info.version;
        Build.saw(info);
        renderComposer();
        // The same switch decides whether a session can be started, and it can change under an
        // open sheet — this arrives again on every reconnect.
        Start.sync();
    },
    conn: function (state, seconds) {
        S.conn = state;
        S.retryIn = seconds || 0;
        // A connection that has stopped trying is no longer a wait; it is an answer, and one of
        // the empty states says it in words. A skeleton with nothing coming is worse than the
        // sentence it was standing in for, because it never admits that it has given up.
        if (state !== "connecting") Waits.list.settle(function () {
            // Nothing ever arrived and the wait has given up: what was held blank while an answer
            // might still have been coming *is* the answer now, and all four screens say it.
            if (!S.arrived && !S.openId) { render(); renderTranscript(); }
            else renderList();
        });
        renderConn();
    }
};

/* ---- the real one -------------------------------------------------------- */

function jsonFetch(path, options) {
    return fetch(path, options).catch(function () {
        // `fetch` rejects with "Failed to fetch" and nothing else when there is no server at the
        // other end. That string ends up in front of somebody as an explanation, so it is turned
        // into one here rather than in each of the five places that show a message.
        var dead = new Error(T.webOffline);
        dead.code = "offline";
        throw dead;
    }).then(function (res) {
        return res.text().then(function (body) {
            var data = null;
            try { data = body ? JSON.parse(body) : null; } catch (e) { /* below */ }
            if (!res.ok) {
                var err = data && data.error ? data.error : { code: "http_" + res.status, message: res.statusText || T.webRequestFailed };
                var e2 = new Error(err.message || err.code);
                e2.code = err.code;
                // `code` and `message` are on every refusal this server makes; a route may add
                // one more field, and dropping it here would cost the page its own sentence.
                // Today there is exactly one: both 409s from starting a session carry `app`,
                // the terminal's name as macOS spells it, so that a phone can say "Ghostty is
                // not running" in the reader's language instead of showing the English.
                if (typeof err.app === "string") e2.app = err.app;
                throw e2;
            }
            if (!data) throw new Error(T.webNotJSON);
            return data;
        });
    });
}

/** A POST of JSON, which is the shape every write on this server takes. */
function post(body, extra) {
    var headers = { "Content-Type": "application/json" };
    for (var k in (extra || {})) headers[k] = extra[k];
    return { method: "POST", headers: headers, body: JSON.stringify(body || {}) };
}

/**
 * A token handed to this page in the URL fragment, traded for a cookie.
 *
 * The Settings window on the Mac mints one and opens `#t=…`. A fragment is the right envelope
 * for a credential: browsers do not send it to the server and do not write it into access logs.
 * The trade is needed because **`EventSource` cannot carry a header** — a token held in a
 * variable can fetch, but it cannot open the stream that everything else here depends on.
 *
 * The fragment is wiped before the request goes out rather than after it comes back. What is
 * being removed is a live credential sitting in an address bar on somebody's screen, and there
 * is no version of "wait for the round trip" that is better than that.
 */
function adoptToken() {
    var match = /(?:^|[#&])t=([^&]+)/.exec(location.hash || "");
    if (!match) return Promise.resolve(false);
    var token = decodeURIComponent(match[1]);
    stripFragment();
    if (MOCK) return Promise.resolve(true);
    return jsonFetch("/v1/auth/adopt", post({ token: token }))
        .then(function () { return true; })
        .catch(function () { return false; });   // the door is a moment away and says it better
}

function stripFragment() {
    try {
        history.replaceState(null, "", location.pathname + location.search);
    } catch (e) {
        location.hash = "";                      // older Safari from a file:// copy
    }
}

var Live = {
    es: null,
    attempt: 0,
    timer: null,
    countdown: null,

    start: function () {
        var self = this;
        adoptToken().then(function () { self.check(); });
    },

    /**
     * Health first, and again before every reconnect.
     *
     * It is the one route that answers without a token, which makes it the only way to tell
     * "this browser is not allowed in" from "the app is not running". `EventSource` reports a
     * failure and never a reason, so a page that only retried the stream would show a locked-out
     * reader a counter going round forever with nothing on it about pairing.
     */
    check: function () {
        var self = this;
        // Never from the cache. This one answer decides whether the reader is shown the door or
        // the sessions, and a stale "yes you are signed in" is the wrong answer to have kept.
        return jsonFetch("/v1/health", { cache: "no-store" }).then(function (h) {
            handlers.hello(h);
            // Only offer the password door when there is a password behind it. `auth` is true for
            // a paired device as well, so it cannot answer this — and a link to a door that was
            // never built teaches somebody to distrust the rest of the page.
            if (els["door-to-password"]) {
                els["door-to-password"].hidden = h.password !== true;
            }
            if (h.authed === false) { self.stop(); Door.show(); return; }
            Door.hide();
            self.begin();
        }).catch(function () {
            // Not even health. Whatever is wrong is not about permission, so let the stream's own
            // backoff be the thing that says so.
            self.begin();
        });
    },

    begin: function () {
        var self = this;
        jsonFetch("/v1/sessions").then(function (d) {
            handlers.sessions(d.sessions, d.at);
        }).catch(function (e) {
            if (e.code === "unauthorized") { self.stop(); Door.show(); }
        });
        // Once, here; the stream keeps it true afterwards and sends a frame of its own the
        // moment it opens. **The refusal is swallowed on purpose**: an app without the route is
        // a 404, and the honest answer to "this Mac has no orchestrator" is the list as it was.
        this.tasks().then(function (d) { handlers.tasks(d.tasks); }).catch(function () { });
        if (!this.es) this.connect();
    },

    /** Stand down: the door is up, and a stream we are not allowed to open is just noise. */
    stop: function () {
        this.clearTimers();
        if (this.es) { this.es.close(); this.es = null; }
        this.attempt = 0;
        handlers.conn("locked");
    },

    connect: function () {
        var self = this;
        this.clearTimers();
        handlers.conn("connecting");
        var es = new EventSource("/v1/events");
        this.es = es;

        es.addEventListener("hello", function (ev) {
            try { handlers.hello(JSON.parse(ev.data)); } catch (e) { }
        });
        es.addEventListener("sessions", function (ev) {
            try {
                var d = JSON.parse(ev.data);
                handlers.sessions(d.sessions, d.at);
            } catch (e) { }
            // A payload that parsed is the only proof the connection actually works; onopen
            // fires for a socket that a proxy may still be holding open with nothing behind it.
            self.attempt = 0;
            handlers.conn("live");
        });
        // Dispatched work. Its own event because it moves on its own clock — a task is briefed
        // and finishes without the session list changing at all — and an app that has never
        // heard of it simply never sends one.
        es.addEventListener("orchestrator", function (ev) {
            try { handlers.tasks(JSON.parse(ev.data).tasks); } catch (e) { }
        });
        es.onopen = function () { handlers.conn("live"); };
        es.onerror = function () {
            // EventSource has a reconnect policy of its own, and it is not one that can be seen
            // or slowed down. Closing it and coming back on our own terms is what makes the
            // backoff visible in the header instead of being a silent loop in the browser.
            es.close();
            if (self.es === es) self.es = null;
            self.retry();
        };
    },

    retry: function () {
        var self = this;
        // Anything already scheduled goes first. Two errors arriving close together used to leave
        // an orphaned countdown interval with no handle, writing "retrying in 12s" over a chip
        // that had since been told something truer.
        this.clearTimers();
        var base = Math.min(30000, 1000 * Math.pow(2, this.attempt));
        // Jitter, because every page open on this machine reconnects at the same instant
        // otherwise — the app restarting is exactly the moment they all lost the stream.
        var wait = Math.round(Math.min(30000, base * (0.7 + Math.random() * 0.6)));
        this.attempt = Math.min(this.attempt + 1, 6);

        var left = Math.round(wait / 1000);
        handlers.conn(left > 1 ? "retrying" : "connecting", left);
        this.countdown = setInterval(function () {
            left -= 1;
            handlers.conn(left > 1 ? "retrying" : "connecting", Math.max(left, 0));
        }, 1000);
        // Back through health rather than straight at the stream: a cookie revoked while the page
        // was open is a 401 the stream can never spell out, and this is where it becomes the door.
        this.timer = setTimeout(function () { self.check(); }, wait);
    },

    clearTimers: function () {
        clearTimeout(this.timer); clearInterval(this.countdown);
        this.timer = this.countdown = null;
    },

    /** Ask again by hand — the phone's pull-to-refresh, and the connection chip. */
    refresh: function () {
        var self = this;
        if (!this.es) { this.clearTimers(); this.attempt = 0; return this.check(); }
        return jsonFetch("/v1/sessions").then(function (d) { handlers.sessions(d.sessions, d.at); })
            .catch(function (e) { toast(e.message, true); });
    },

    transcript: function (id) {
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/transcript?limit=200");
    },

    /// Every task the app knows about — what a session dispatched, and which session got it.
    /// Read-only from here whatever this device is: dispatching needs a credential that never
    /// leaves the Mac, so there is nothing this page could ask for that would start one.
    tasks: function () { return jsonFetch("/v1/orchestrator/tasks"); },

    /// One of that session's background agents: the row the strip is already showing, and the
    /// conversation behind it. Same shape as a transcript, because it is one.
    agent: function (id, agentId) {
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/agents/"
                         + encodeURIComponent(agentId) + "?limit=200");
    },

    skills: function (id) {
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/skills");
    },

    /// `text`, `images`, or both — the server refuses only the message that is neither. The key
    /// is minted here, once per send, so that a retry of *this* request is not a second prompt.
    send: function (id, text, images) {
        var body = {};
        if (text) body.text = text;
        if (images && images.length) body.images = images;
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/send",
                         post(body, { "Idempotency-Key": uuid() }));
    },

    /// Answer a menu with the keystroke it is numbered with.
    ///
    /// **Not `send`.** Claude Code's picker discards a bracketed paste and acts on the Return
    /// that follows it, so words posted to a session showing a menu confirm whichever row is
    /// highlighted rather than typing anything — the server refuses that outright. A digit is
    /// the only thing that answers the question that was actually asked.
    key: function (id, press) {
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/key",
                         post({ key: String(press) }, { "Idempotency-Key": uuid() }));
    },

    focus: function (id) {
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/focus",
                         post({}, { "Idempotency-Key": uuid() }));
    },

    /// Leave through the assistant's own prompt, then close the terminal session it occupied.
    /// The server knows whether that means `/exit` or `/quit`; the page never sends a command.
    end: function (id) {
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/end",
                         post({}, { "Idempotency-Key": uuid() }));
    },

    /// Branch and file changes are also fetched only when their panel opens. Unlike the command
    /// actions in the same menu this is read-only, and it never rides the event stream.
    git: function (id) {
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/git");
    },

    /// Where a session may be started. Asked afresh every time the sheet opens: the Mac drops
    /// directories that are no longer there while it builds this, and the answer is only as
    /// true as the moment it was given.
    /// The facts behind the compact status line and its expanded card: what the session is on,
    /// what it has spent, what is left of the plan's window, how much changed on disk, whether
    /// the last deploy went out. Kept out of the session-list stream because answering reads a
    /// transcript that can be fifty megabytes; the client gives the answer a short-lived cache.
    info: function (id) {
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/info");
    },

    places: function () { return jsonFetch("/v1/places"); },

    /// The id and the assistant are the whole request, and **both of them are in the path**.
    /// There is no body on this route — not "an optional body", none is read — so there is
    /// nothing this page could send that would widen what gets started, and the command at the
    /// other end is a literal off a two-case enum. An assistant this page invented is a 404 at
    /// the Mac rather than a string that reaches a shell. The key is minted once per press,
    /// which is what makes a retry of *this* request the same start rather than a second tab.
    ///
    /// Named for what it starts. `start` on this object is the transport's own — the one `boot`
    /// calls to open the stream — and a second one would have quietly replaced it.
    startPlace: function (id, assistant) {
        var path = "/v1/places/" + encodeURIComponent(id) + "/start";
        if (assistant) path += "/" + encodeURIComponent(assistant);
        return jsonFetch(path, post({}, { "Idempotency-Key": uuid() }));
    },

    // The three doors. None of them carries the code: it is shown on the Mac, and this page is
    // only ever the thing typing it back.
    pair: function (name) { return jsonFetch("/v1/auth/pair", post({ name: name })); },
    confirmPair: function (id, code) {
        return jsonFetch("/v1/auth/pair/confirm", post({ pairing_id: id, code: code }));
    },
    password: function (password, name) {
        return jsonFetch("/v1/auth/password", post({ password: password, name: name }));
    },

    // Web Push. The subscription goes up exactly as the browser wrote it — it is the browser's
    // own object, endpoint and keys, and anything this end reshaped would be a chance to get a
    // credential wrong on the way past.
    pushKey: function () { return jsonFetch("/v1/push/key"); },
    pushSubscribe: function (subscription) { return jsonFetch("/v1/push/subscribe", post(subscription)); },
    pushUnsubscribe: function (id) { return jsonFetch("/v1/push/unsubscribe", post({ id: id })); },
    /// Reaches this device and nothing else — the server sends only to the subscriptions the
    /// asking device owns, so pressing this on a phone buzzes that phone and nobody else's.
    pushTest: function () { return jsonFetch("/v1/push/test", post({})); }
};

/* ---- fixtures ------------------------------------------------------------
   Enough of a machine to see every state, every animation and the reconnect —
   from a file:// copy with nothing running. It is also how this page was
   checked, which is the actual reason it exists.
   -------------------------------------------------------------------------- */

var Mock = (function () {
    var C = "#d97757", O = "#141416", BG = "#33201a";          // the clawdline mark
    var W = "#eef6f4", TEAL = "#2f6b5e";                        // atrium
    var BODY = "#5aa6d8", LIMB = "#2f6b95";                     // a generated creature

    function art(rows, palette, bg) {
        return rows.map(function (row) {
            return row.split("").map(function (ch) { return ch === "." ? bg : (palette[ch] || bg); });
        });
    }

    var clawdline = {
        accent: C,
        cells: art([".######.", ".#o##o#.", "########", ".##..##."], { "#": C, "o": O }, BG)
    };
    var atrium = {
        accent: "#5cbba1",
        cells: art([".WWWWW.", ".W...W.", ".W.W.W.", ".W...W."], { "W": W }, TEAL)
    };
    var creature = {
        accent: BODY,
        cells: [[LIMB, null, LIMB, null, LIMB],
                [BODY, BODY, BODY, BODY, BODY],
                [BODY, null, BODY, null, BODY],
                [LIMB, null, LIMB, null, LIMB]]
    };

    var now = Math.floor(Date.now() / 1000);
    var sessions = [
        // Working, with three agents out — the case the terminal cannot show at all, and the
        // reason `Subagents` exists. One of them has just landed and is saying what it found.
        { id: "8F3A-1C", backend: "iterm", tty: "ttys004", label: "investigate the webhook",
          cwd: "/Users/x/code/clawdline", state: "working", line: "Gallivanting… (2m 4s · ↓ 6.4k tokens)",
          isClaude: true, assistant: "claude", sessionId: "a2937509-a3d4-4c31-87a7-cdb7ff073d38", icon: clawdline,
          agents: [
              { id: "a1", what: "Search the delivery logs", type: "general-purpose",
                state: "running", depth: 1, at: now - 8, doing: "Grep: retry_after" },
              { id: "a2", what: "Read the signing middleware", type: "general-purpose",
                state: "running", depth: 1, at: now - 31, doing: "Read: app/webhooks/verify.rb" },
              { id: "a3", what: "Check the queue depth", type: "general-purpose",
                state: "done", depth: 1, at: now - 12,
                result: "Depth is flat at 0 — nothing is backing up.",
                tokens: 18420, tools: 5, seconds: 44.2 }
          ] },
        // Waiting, **with the question in it**. This is what the phone could never see: the
        // options were parsed on the Mac and thrown away, so the box could only say "go and
        // find the Mac". The caret is on the second row, which is what a bare Return confirms.
        { id: "2C71-90", backend: "iterm", tty: "ttys011", label: "the signup flow keeps 500ing",
          cwd: "/Users/x/code/atrium", state: "waiting", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: atrium,
          menu: { selected: 2, options: [
              { n: 1, label: "Yes", selected: false, can: true },
              { n: 2, label: "Yes, and don't ask again for rails commands in atrium",
                selected: true, can: true },
              { n: 3, label: "No, tell Claude what to do instead", selected: false, can: true }
          ] } },
        { id: "9B04-2D", backend: "iterm", tty: "ttys002", label: "rewrite the CSV importer",
          cwd: "/Users/x/code/notebook", state: "idle", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: creature },
        { id: "A15E-77", backend: "tmux", tty: "tmux:%12", label: "docs pass before the release",
          cwd: "/Users/x/code/clawdline/docs", state: "idle", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: clawdline },
        { id: "44D2-05", backend: "iterm", tty: "ttys017", label: "scratch",
          cwd: "/Users/x/tmp/notes", state: "idle", line: null,
          isClaude: false, assistant: "codex", sessionId: null, icon: null },
        { id: "C0FF-3E", backend: "iterm", tty: "ttys021", label: "build box over ssh",
          cwd: "/Users/x", state: "unknown", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: null }
    ];

    // What the top session sent away. Both of them hang off `8F3A-1C`, and their children are
    // two of the rows above — which is the only way to see the grouping work: the codex row is
    // near the bottom of the list on its own merits and belongs directly under its root.
    //
    // One of each kind that matters. The codex one is still going and has no cost to show —
    // that plan is not billed per token — and the Claude one has landed, which is what puts a
    // figure in the header. A task that finished ten minutes ago stops moving its row; this one
    // is three minutes old, so it is still drawn under the session that asked for it.
    var tasks = [
        { id: "6f1b3d84-2a17-4c95-91ce-70b5a4e2d011", state: "briefed", kind: "image",
          title: "Draw the project portrait", assistant: "codex", projectDir: "/Users/x/tmp/notes",
          created: now - 260, spawnedAt: now - 254, briefedAt: now - 248, finishedAt: null,
          dir: "/tmp/.clawdline/6f1b3d84-2a17-4c95-91ce-70b5a4e2d011",
          root: { sessionId: "a2937509-a3d4-4c31-87a7-cdb7ff073d38",
                  label: "investigate the webhook", terminalId: "8F3A-1C" },
          child: { terminalId: "44D2-05", backend: "iterm", sessionId: null },
          artifacts: [],
          usage: { input: 21400, output: 3100, cacheRead: 88000, cacheWrite: 1900,
                   total: 114400, model: "gpt-5.1-codex", costUsd: null } },
        { id: "b70e51c9-84af-4d2e-a6d1-1c2f9e330447", state: "success", kind: "code-review",
          title: "Read the docs pass for holes", assistant: "claude",
          projectDir: "/Users/x/code/clawdline/docs",
          created: now - 900, spawnedAt: now - 894, briefedAt: now - 888, finishedAt: now - 180,
          dir: "/tmp/.clawdline/b70e51c9-84af-4d2e-a6d1-1c2f9e330447",
          root: { sessionId: "a2937509-a3d4-4c31-87a7-cdb7ff073d38",
                  label: "investigate the webhook", terminalId: "8F3A-1C" },
          child: { terminalId: "A15E-77", backend: "tmux", sessionId: "1f0c8a44-9d21-4b6e-8f30-2ab7c5e91d62" },
          summary: "Three sentences say where the file goes and none of them agree.",
          artifacts: ["artifacts/notes.md"],
          usage: { input: 9600, output: 4200, cacheRead: 61000, cacheWrite: 800,
                   total: 75600, model: "claude-sonnet-4-5", costUsd: 0.0913 } }
    ];

    // The line that cannot wrap, five hundred characters of it, because this is the shape that
    // broke the page and there is no waiting around for a real session to print another one. A
    // phone showed it as a black screen with one strip of garbage floating in the middle: nothing
    // in the string could break, so it stretched the transcript column past the edge of the
    // display and left every readable line stranded off to one side. Escape codes were the first
    // producer of these and are now stripped upstream; a base64 blob or a long path is the same
    // string with a different story, so the fixture keeps one.
    var unbreakable = "aGVsbG8td29ybGQtdGhpcy1pcy1vbmUtdmVyeS1sb25nLXRva2Vu".repeat(10);

    // One session gets a real transcript: twenty-odd entries, prose and tools mixed, with
    // several runs of tool calls back to back — which is what the folding has to hold up
    // under, and the reason the fixture is worth having.
    var script = [
        ["user", "the webhook from stripe is landing twice on staging. same event id, two rows.", null],
        ["assistant", "Two rows for one `event.id` means either the handler is not idempotent or it is being invoked twice. Let me look at where the row is written before guessing.", null],
        ["tool", "Sources/Webhook.swift, Sources/Ledger.swift", "Grep"],
        ["tool", "", "Read"],
        ["assistant", "The insert is unconditional:\n\n```swift\ntry db.insert(Payment(eventID: event.id, amount: event.amount))\n```\n\nNothing consults `eventID` first, so a redelivery writes a second row. Stripe redelivers whenever our 200 arrives late.", null],
        ["user", "how late is late", null],
        ["assistant", "Stripe waits 30 seconds for the ack. Anything slower is treated as a failure and queued for retry — the work still finished on our side, which is exactly how you get one event and two rows.", null],
        ["tool", "grep -n \"await\" Sources/Webhook.swift", "Bash"],
        ["assistant", "The handler does the ledger write, then sends the receipt email, then answers. The email is the slow part and it is on the wrong side of the reply.", null],
        ["user", "so ack first, then work?", null],
        ["assistant", "That is half of it. Acking first stops the redelivery, but a crash between the ack and the write would then lose the payment silently — which is a worse bug than the one being fixed. Both halves: a unique index on `event_id` so a second write cannot land, and the email moved off the request.", null],
        ["tool", "", "Edit"],
        ["tool", "Sources/Ledger.swift", "Edit"],
        ["assistant", "Added the unique index and made the insert an upsert on `event_id`. The handler now answers as soon as the row is durable.", null],
        ["tool", "swift test --filter WebhookTests", "Bash"],
        ["tool", "", "Read"],
        ["assistant", "Eleven tests, all passing, including a new one that posts the same event twice and asserts one row.", null],
        ["user", "add one for the crash-between case", null],
        ["assistant", "Done — it kills the process after the insert and before the email, restarts, and checks that the receipt is still sent from the queue rather than lost.", null],
        ["tool", "swift test", "Bash"],
        ["assistant", "Green. The migration is `0009_event_id_unique.sql`; it will need running on staging before the next deploy or the index will not be there to protect anything.", null],
        // On its own between two answers, so it is never folded away: the point of it is to be
        // on screen. Both halves of the problem are here — the result below, and the same run of
        // characters inside a sentence, which is where a wrapping rule that only covers `.body`
        // and forgets its children falls over.
        ["tool", "curl -s /v1/artifacts/" + unbreakable, "Bash"],
        ["assistant", "The upload came back as `" + unbreakable + "` — one token, no spaces, and nowhere for a line to break.", null],
        ["user", "summarise what changed", null],
        // Everything the renderer knows how to draw, in one answer, so the fixture is also the
        // test: a table with alignment markers, a heading, both kinds of list, a quote, a rule.
        ["assistant", "## What changed\n\n| file | change | lines |\n|---|:---:|---:|\n| `Sources/Webhook.swift` | ack before the email | **+18 −24** |\n| `Sources/Ledger.swift` | upsert on `event_id` | +7 −2 |\n| `0009_event_id_unique.sql` | new | +4 |\n\n### Still to do\n\n1. Run the migration on staging\n2. Watch the redelivery counter for a day\n   - it should fall to zero\n   - if it does not, the index did not take\n\n> The handler now answers as soon as the row is durable, and *not* before.\n\n**Run `0009_event_id_unique.sql` on staging first** — until it has run there is no index, and nothing stopping the second row.\n\n---\n\nSee [the Stripe note on retries](https://stripe.com/docs/webhooks) for where the 30 seconds comes from.", null]
    ];

    var transcripts = {};
    transcripts["8F3A-1C"] = script.map(function (e, i) {
        return { role: e[0], text: e[1], tool: e[2], at: now - (script.length - i) * 47 };
    });
    // A question, in the shape the wire carries one: the marker, then the questions as data.
    // See `ASK_MARK`. The fixture is also where the block's layout is worked on, so it has both
    // kinds — one answer wanted, and any number of them.
    function asked(questions, at) {
        return { role: "tool", tool: "AskUserQuestion", text: ASK_MARK + JSON.stringify(questions), at: at };
    }

    transcripts["2C71-90"] = [
        { role: "user", text: "run the migration on staging", tool: null, at: now - 220 },
        { role: "assistant", text: "This drops `payments_legacy`. Before I run it: the table still has 12,041 rows and nothing in the repo reads it.", tool: null, at: now - 180 },
        asked([{
            h: "Migration",
            q: "`0009_event_id_unique.sql` drops `payments_legacy`, which still holds 12,041 rows. How shall I run it?",
            o: [
                { l: "Copy the table first, then run it", d: "`payments_legacy_2026_08` beside it, then the migration. Nothing is lost and the copy can go next week." },
                { l: "Run it as it is", d: "The table goes. Nothing in the repository reads it, and staging has last night's dump behind it." },
                { l: "Stop here", d: "Leave staging as it is and I will write the copy step into the migration instead." }
            ]
        }], now - 170)
    ];
    transcripts["44D2-05"] = [
        { role: "user", text: "get the release notes ready", tool: null, at: now - 900 },
        asked([{
            h: "Release notes",
            m: true,
            q: "Which of these are worth a line in the notes?",
            o: [
                { l: "The webhook fix", d: "One event, one row. The one somebody wrote in about." },
                { l: "Faster session list", d: "A `ps` per comparison became a `ps` per file." },
                { l: "New icons", d: "Cosmetic, and nobody asked." }
            ]
        }], now - 880)
    ];
    transcripts["A15E-77"] = [
        { role: "user", text: "read docs/devstack.md and tell me what is missing", tool: null, at: now - 3600 },
        { role: "tool", text: "docs/devstack.md", tool: "Read", at: now - 3590 },
        { role: "assistant", text: "It explains the file and never says where to put it. Everything else assumes you already know that `devstack.json` sits at the root of the project it describes.", tool: null, at: now - 3580 }
    ];

    // Where a session may be started — the Mac's own list, which is not the session list and is
    // not the icon registry either: directories `claude` has been run in that are still there,
    // newest first. Ten of them, because that is enough to put the filter on screen.
    var places = [
        { id: "3b9e26c1587facfd", label: "clawdline", path: "/Users/you/code/clawdline", icon: clawdline },
        { id: "24f9bac626da56ea", label: "atrium", path: "/Users/you/code/atrium", icon: atrium },
        { id: "470885724e5330e1", label: "cairn", path: "/Users/you/code/cairn", icon: creature },
        { id: "9c1d4e77a0b3f215", label: "notebook", path: "/Users/you/code/notebook", icon: creature },
        { id: "b04f8a2c6d915e33", label: "astro", path: "/Users/you/code/astro", icon: null },
        { id: "5e7a1c93f2680b4d", label: "frontend", path: "/Users/you/code/cairn/frontend", icon: creature },
        { id: "1f6b0d38e9a742c5", label: "docs", path: "/Users/you/code/clawdline/docs", icon: clawdline },
        { id: "c83e5f10ab24d967", label: "sketches", path: "/Users/you/code/sketches", icon: null },
        { id: "7a2c9e46b1d05f38", label: "website", path: "/Users/you/code/website", icon: null },
        { id: "e51b7d02c4a86f19", label: "notes", path: "/Users/you/tmp/notes", icon: null }
    ];
    // What a project has an address for, per session. Two of the six have anything at all,
    // which is the honest proportion: a project with no CI and no dev stack has an empty list,
    // and that is the case the sheet mostly has to be good at.
    //
    // Between them the rows cover every shape the sheet has to draw — a site that is up, a
    // deploy that has failed, one that is still running, two dev-stack servers on the Mac's own
    // network with one of them down, and a backlog that is a file no browser can open.
    var links = {
        "8F3A-1C": [
            { label: "site", url: "https://clawdline.example.com", kind: "site", state: "ok", local: false },
            { label: "ci", url: "https://github.com/example/clawdline/actions/runs/32206093368", kind: "deploy", state: "fail", local: false },
            { label: "web", url: "http://127.0.0.1:5173", kind: "server", state: "ok", local: true },
            { label: "api", url: "http://127.0.0.1:8787", kind: "server", state: "down", local: true, why: "build-web: bash: npm: command not found" },
            { label: "backlog", url: "file:///Users/you/code/clawdline/artifacts/backlog.html", kind: "artifact", state: "", local: true }
        ],
        "2C71-90": [
            { label: "staging", url: "https://staging.example.com", kind: "site", state: "down", local: false },
            { label: "deploy", url: "https://github.com/example/atrium/actions/runs/32206093412", kind: "deploy", state: "running", local: false }
        ]
    };
    // The info card's shapes. A Claude session that has just spent its five-hour window, with
    // the deploy that failed under it; a Claude session whose plan nobody has reported on, in a
    // clean tree; and a Codex session with the plain percentage its rollout carries. Every
    // other session answers with what a session that has no transcript yet answers with.
    // `?info=fail` is the route refusing.
    // What each assistant's `/model` will take: Claude Code an alias, Codex a slug.
    var CLAUDE_MODELS = [
        { id: "claude-fable-5", name: "Fable 5", command: "fable" },
        { id: "claude-opus-5", name: "Opus 5", command: "opus" },
        { id: "claude-sonnet-5", name: "Sonnet 5", command: "sonnet" },
        { id: "claude-haiku-4-5", name: "Haiku 4.5", command: "haiku" }
    ];
    var CODEX_MODELS = [
        { id: "gpt-5.6-sol", name: "GPT-5.6-Sol", command: "gpt-5.6-sol" },
        { id: "gpt-5.5", name: "GPT-5.5", command: "gpt-5.5" },
        { id: "gpt-5.4", name: "GPT-5.4", command: "gpt-5.4" },
        { id: "gpt-5.4-mini", name: "GPT-5.4-Mini", command: "gpt-5.4-mini" }
    ];
    var info = {
        "8F3A-1C": {
            models: CLAUDE_MODELS,
            session: { id: "8F3A-1C", assistant: "claude", sessionId: "a2937509-a3d4-4c31-87a7-cdb7ff073d38",
                       model: "claude-fable-5", cwd: "/Users/x/code/clawdline",
                       startedAt: now - 5580, seconds: 5580 },
            usage: { input: 4821, output: 38210, cacheRead: 2984120, cacheWrite: 214880, total: 3242031,
                     model: "claude-fable-5", costUsd: 7.38 },
            limits: { windows: [
                { name: "5h", usedPercent: 100, resetsAt: now + 2760, hit: true }
            ], at: now - 90 },
            files: { branch: "main", head: "d5c61e9f91c46a77", ahead: 2, behind: 0,
                     staged: 1, unstaged: 4, untracked: 2, conflict: 0 },
            links: links["8F3A-1C"]
        },
        "2C71-90": {
            models: CLAUDE_MODELS,
            session: { id: "2C71-90", assistant: "claude", model: "claude-opus-5", cwd: "/Users/x/code/atrium",
                       startedAt: now - 24300, seconds: 24300 },
            usage: { input: 19340, output: 61022, cacheRead: 7120400, cacheWrite: 380210, total: 7580972,
                     model: "claude-opus-5", costUsd: 5.61 },
            limits: { windows: [] },
            files: { branch: "fix/signup-500", head: "9a1b2c3d4e5f6071", ahead: 0, behind: 3,
                     staged: 0, unstaged: 0, untracked: 0, conflict: 0 },
            links: links["2C71-90"]
        },
        "44D2-05": {
            models: CODEX_MODELS,
            session: { id: "44D2-05", assistant: "codex", model: "gpt-5.3-codex", cwd: "/Users/x/tmp/notes",
                       startedAt: now - 840, seconds: 840 },
            usage: { input: 8190546, output: 16956, cacheRead: 7978752, cacheWrite: 0, total: 8207502,
                     model: "gpt-5.3-codex" },
            limits: { windows: [
                { name: "5h", usedPercent: 24, resetsAt: now + 9000, hit: false },
                { name: "7d", usedPercent: 71, resetsAt: now + 3 * 86400 + 4000, hit: false }
            ], at: now - 12 },
            links: []
        }
    };
    var MOCK_INFO = params.get("info") || "";
    var MOCK_GIT = params.get("git") || "";

    // How the fixture's start behaves: `?start=slow` never reports in, so the fifteen seconds
    // can be watched rather than described; `closed`, `unsupported` and `gone` are the three
    // refusals with a sentence of their own on the other end, and `empty` is a Mac with
    // nowhere to start at all.
    var MOCK_START = params.get("start") || "";

    var live = false;
    var beat = 0;
    var admitted = !MOCK_DOOR;   // ?door=1 arrives with nothing, the way a phone does
    var asked = 0, wrong = 0;

    function emit() {
        if (!live) return;
        // A copy each time, for the same reason the server sends the whole list: nothing
        // downstream should be able to hold a reference into the source of truth.
        handlers.sessions(JSON.parse(JSON.stringify(sessions)), Math.floor(Date.now() / 1000));
    }

    function find(id) {
        for (var i = 0; i < sessions.length; i++) if (sessions[i].id === id) return sessions[i];
        return null;
    }

    var verbs = ["Gallivanting", "Puzzling", "Noodling", "Percolating", "Untangling", "Reticulating"];
    var clocks = {};

    /** The live line carries its own clock, written by Claude Code. Ticking it here is what
     *  makes a working row look alive between the bigger changes. */
    function tick() {
        var changed = false;
        sessions.forEach(function (s) {
            if (s.state !== "working") { delete clocks[s.id]; return; }
            var c = clocks[s.id] || (clocks[s.id] = { since: Date.now(), verb: verbs[Math.floor(Math.random() * verbs.length)], tokens: 1.2 + Math.random() * 4 });
            var secs = Math.floor((Date.now() - c.since) / 1000);
            c.tokens += 0.04;
            s.line = c.verb + "… (" + (secs >= 60 ? Math.floor(secs / 60) + "m " + (secs % 60) + "s" : secs + "s") +
                " · ↓ " + c.tokens.toFixed(1) + "k tokens)";
            changed = true;
        });
        if (changed) emit();
    }

    /** The interesting part: states that move, so the transitions can be watched. */
    function step() {
        beat += 1;
        var order = [
            function () { find("9B04-2D").state = "working"; },
            function () { find("8F3A-1C").state = "idle"; find("8F3A-1C").line = null;
                          transcripts["8F3A-1C"].push({ role: "assistant", text: "Pushed to `fix/webhook-idempotency`. Nothing else is outstanding.", tool: null, at: Math.floor(Date.now() / 1000) }); },
            function () { find("44D2-05").state = "waiting"; },
            function () { find("2C71-90").state = "working"; find("C0FF-3E").state = "idle"; },
            function () { find("9B04-2D").state = "waiting"; find("44D2-05").state = "idle"; },
            function () { find("2C71-90").state = "waiting"; find("8F3A-1C").state = "working"; },
            function () { find("9B04-2D").state = "idle"; find("C0FF-3E").state = "unknown"; }
        ];
        order[beat % order.length]();
        emit();
    }

    return {
        start: function () {
            handlers.conn("connecting");
            if (!admitted) {
                setTimeout(function () { handlers.conn("locked"); Door.show(); }, 300);
                return;
            }
            setTimeout(function () {
                live = true;
                handlers.hello({ version: "0.6.0-mock", protocol: 1, write: MOCK_WRITE, auth: true, authed: true });
                handlers.conn("live");
                emit();
                // What the stream's own `orchestrator` frame carries, at the moment it carries
                // it: right after the first list, so the rows are already there to be grouped.
                handlers.tasks(JSON.parse(JSON.stringify(tasks)));
            }, 420);
            setInterval(tick, 1000);
            setInterval(step, 4200);
            if (MOCK_FLAKY) {
                setInterval(function () {
                    live = false;
                    handlers.conn("retrying", 3);
                    setTimeout(function () { live = true; handlers.conn("live"); emit(); }, 3000);
                }, 20000);
            }
        },
        refresh: function () { return new Promise(function (done) { setTimeout(function () { emit(); done(); }, 500); }); },

        /// The same two records the fixture's stream sends, for the one fetch that happens
        /// before the stream is open. A copy, for the reason `emit` makes one.
        tasks: function () {
            return new Promise(function (done) {
                setTimeout(function () { done({ tasks: JSON.parse(JSON.stringify(tasks)) }); }, 200);
            });
        },

        // The door, fixtured. The code is 424242 and the password is "mock" — said out loud here
        // because a fixture that keeps secrets from the person testing it is only a nuisance, and
        // `mock_code` is a field no real server sends.
        pair: function (name) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    asked += 1;
                    if (asked > 3) {
                        fail(Object.assign(new Error("Too many pairing attempts. Try again in a few minutes."),
                                           { code: "rate_limited" }));
                        return;
                    }
                    wrong = 0;
                    // `?expires=5` shortens the two minutes so the lapse can be watched rather
                    // than waited out.
                    var life = parseInt(params.get("expires"), 10) || 120;
                    done({ pairing_id: "mock-" + name, expires: Math.floor(Date.now() / 1000) + life,
                           mock_code: "424242" });
                }, 350);
            });
        },
        confirmPair: function (id, code) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (code === "424242") { admitted = true; done({ ok: true }); return; }
                    wrong += 1;
                    fail(Object.assign(new Error(wrong >= 5
                        ? "That pairing has expired. Start again."
                        : "That code is not right. " + (5 - wrong) + " tries left."), { code: "forbidden" }));
                }, 300);
            });
        },
        password: function (secret) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (secret === "mock") { admitted = true; done({ ok: true }); return; }
                    fail(Object.assign(new Error("That is not the password."), { code: "unauthorized" }));
                }, 300);
            });
        },
        transcript: function (id) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    var entries = transcripts[id] || [];
                    if (!entries.length) { fail(Object.assign(new Error("Nothing to read from this session yet."), { code: "not_found" })); return; }
                    done({ entries: entries.slice(), signature: id + ":" + entries.length + ":" + (entries[entries.length - 1] || {}).at });
                }, 220);
            });
        },
        // Enough of an agent's conversation to see the pane it lands in. The row itself comes
        // from the session fixture, so what the header says here is what the strip said.
        agent: function (id, agentId) {
            return new Promise(function (done) {
                setTimeout(function () {
                    var s = sessions.filter(function (x) { return x.id === id; })[0];
                    var meta = ((s && s.agents) || []).filter(function (a) { return a.id === agentId; })[0] || null;
                    var at = Math.floor(Date.now() / 1000) - 90;
                    done({
                        agent: meta,
                        entries: [
                            { role: "user", text: (meta && meta.what) || "Go and have a look.", at: at },
                            { role: "tool", tool: "Grep", text: "retry_after", at: at + 3 },
                            { role: "tool", tool: "Read", text: "app/webhooks/verify.rb", at: at + 9 },
                            { role: "assistant", text: (meta && meta.result) || "Still going.", at: at + 44 }
                        ],
                        signature: agentId + ":mock"
                    });
                }, 200);
            });
        },
        skills: function (id) {
            return new Promise(function (done) {
                setTimeout(function () {
                    var session = sessions.filter(function (s) { return s.id === id; })[0];
                    if (session && session.assistant === "codex") {
                        done({ skills: [
                            { name: "openai-docs", description: "Read official OpenAI documentation", source: "system" },
                            { name: "chrome:control-chrome", description: "Control Chrome for local testing", source: "plugin" },
                            { name: "deploy", description: "Deploy this repository safely", source: "project" }
                        ] });
                        return;
                    }
                    done({ skills: [
                        { name: "recap", description: "Summarize the work in this session", source: "project" },
                        { name: "frontend-design", description: "Build a distinctive production interface", source: "personal" },
                        { name: "design:visual", description: "Review layout and visual hierarchy", source: "plugin" }
                    ] });
                }, 180);
            });
        },
        send: function (id, text, images) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    var t = transcripts[id] || (transcripts[id] = []);
                    var carried = (images || []).map(function (_, i) { return "[Image #" + (i + 1) + "]"; }).join(" ");
                    var s = find(id);
                    var alreadyWorking = s && s.state === "working";
                    if (s) { s.state = "working"; s.line = "Accepting your message…"; }
                    emit();
                    done({ ok: true, at: Math.floor(Date.now() / 1000), audit_id: uuid() });
                    // The HTTP answer means the keystrokes reached the Mac, not that the
                    // assistant has written them into its transcript. Keeping that gap in the
                    // fixture makes the browser exercise the same optimistic/reconcile path as
                    // the real app; a busy session makes the gap conspicuously longer.
                    setTimeout(function () {
                        t.push({ role: "user", text: [carried, text].filter(Boolean).join(" "), tool: null, at: Math.floor(Date.now() / 1000) });
                        if (s) s.line = "Reading your message…";
                        emit();
                    }, alreadyWorking ? 7000 : 4000);
                }, 300);
            });
        },
        /** Answering moves the session off `waiting`, which is the whole thing worth seeing
         *  from a file:// copy: the menu goes, the buttons go, and the composer comes back. */
        key: function (id) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    var s = find(id);
                    if (s) { s.state = "working"; s.line = "Deciding\u2026 (1s)"; s.menu = null; }
                    emit();
                    done({ ok: true });
                }, 200);
            });
        },

        focus: function () {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Focus is not enabled on this server."), { code: "write_disabled" })); return; }
                    done({ ok: true });
                }, 150);
            });
        },

        end: function (id) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    sessions = sessions.filter(function (s) { return s.id !== id; });
                    emit();
                    done({ ok: true });
                // The real route waits for the assistant to quit before it closes the tab. A
                // short fixture made the confirmation disappear before its waiting state could
                // be read, so keep this honest enough to inspect from a file:// copy.
                }, 1200);
            });
        },

        /** Slow because the real route reads a transcript and shells out, and the refresh
         *  button has to be seen to be doing something. */
        info: function (id) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (MOCK_INFO === "fail") {
                        fail(Object.assign(new Error("No session named that"), { code: "not_found" }));
                        return;
                    }
                    if (info[id]) { done({ info: info[id] }); return; }
                    var session = sessions.filter(function (s) { return s.id === id; })[0] || {};
                    done({ info: {
                        session: { id: id, assistant: session.assistant, cwd: session.cwd },
                        limits: { windows: [] },
                        links: (links[id] || []).slice(),
                        models: session.assistant === "codex" ? CODEX_MODELS : CLAUDE_MODELS
                    } });
                }, 640);
            });
        },

        git: function () {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (MOCK_GIT === "fail") {
                        fail(Object.assign(new Error("Could not read that repository"),
                                           { code: "git_failed" }));
                        return;
                    }
                    if (MOCK_GIT === "notrepo") {
                        fail(Object.assign(new Error("That session is not inside a Git repository"),
                                           { code: "not_a_repo" }));
                        return;
                    }
                    done({ git: {
                        branch: "main", head: "d5c61e9f91c46a77", ahead: 2, behind: 0,
                        clean: MOCK_GIT === "clean",
                        files: MOCK_GIT === "clean" ? [] : [
                            { path: "Sources/RemoteServer.swift", from: null, staged: false,
                              unstaged: true, kind: "modified", additions: 12, deletions: 3 },
                            { path: "Resources/web/components/session/detail/GitChangesPanel.css",
                              from: "Resources/web/components/session/GitPanel.css", staged: true,
                              unstaged: true, kind: "renamed", additions: 4, deletions: 1 },
                            { path: "notes/status.txt", from: null, staged: false,
                              unstaged: true, kind: "untracked", additions: null, deletions: null },
                            { path: "Sources/Conflict.swift", from: null, staged: true,
                              unstaged: true, kind: "conflict", additions: 8, deletions: 5 }
                        ]
                    } });
                }, 420);
            });
        },

        places: function () {
            return new Promise(function (done) {
                setTimeout(function () {
                    var now = Math.floor(Date.now() / 1000);
                    done({
                        at: now,
                        // `?start=empty` empties the list, which is the other thing this screen
                        // has to be able to say: a Mac that has never run Claude Code anywhere.
                        // Both, so the chooser is on screen in a demo. A real Mac answers with
                        // whichever of them it actually has a home directory for.
                        assistants: [{ id: "claude", label: "Claude Code" }, { id: "codex", label: "Codex" }],
                        places: MOCK_START === "empty" ? [] : places.map(function (p, i) {
                            return { id: p.id, label: p.label, path: p.path, icon: p.icon, at: now - i * 900 };
                        })
                    });
                }, 260);
            });
        },

        /** The real one answers before the session exists, so this does too — and the row turns
         *  up a couple of seconds later, which is the gap the band above the list is for. */
        startPlace: function (id, assistant) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    var place = null;
                    for (var i = 0; i < places.length; i++) if (places[i].id === id) place = places[i];
                    if (!place || MOCK_START === "gone") {
                        fail(Object.assign(new Error("No place named that"), { code: "not_found" }));
                        return;
                    }
                    if (MOCK_START === "closed") {
                        fail(Object.assign(new Error("Ghostty is not running, and this will not launch it for you."),
                                           { code: "terminal_closed", app: "Ghostty" }));
                        return;
                    }
                    if (MOCK_START === "unsupported") {
                        fail(Object.assign(new Error("A session cannot be started in that terminal from here."),
                                           { code: "terminal_unsupported", app: "Ghostty" }));
                        return;
                    }
                    var made = "N" + Math.floor(Math.random() * 9000 + 1000) + "-" + Math.floor(Math.random() * 90 + 10);
                    done({ ok: true, id: made, backend: "iterm", assistant: assistant || "claude",
                           place: place.id, cwd: place.path, at: Math.floor(Date.now() / 1000) });
                    // Not with the reply: the whole point is that the id is answered before
                    // there is a session to go with it. `?start=slow` is the one that never does.
                    if (MOCK_START === "slow") return;
                    setTimeout(function () {
                        sessions.push({ id: made, backend: "iterm", tty: "ttys0" + Math.floor(Math.random() * 90 + 10),
                                        label: place.label, cwd: place.path, state: "idle", line: null,
                                        // Absent for a moment on the real thing too — a shell has
                                        // to start before the assistant is a process anything
                                        // can see, whichever one was asked for.
                                        isClaude: false, assistant: null,
                                        sessionId: null, icon: place.icon });
                        emit();
                    }, 2600);
                }, 320);
            });
        }
    };
})();

// The one API on this page: the real one, or the fixtures. `api` itself lives in `net/api.js`,
// which knows about neither — see the note there.
useApi(MOCK ? Mock : Live);
/* ==========================================================================
   5. The door
   Everything below this line assumes the server is answering. This is what is
   on screen when it is not answering *this browser* — a pairing code that is
   shown on the Mac and typed in here, in that direction and never the other.
   ========================================================================== */

var Door = {

    pairing: null,      // { id, expires } while a code is live on the Mac's screen
    wrong: 0,           // counted here as well as there: the server kills a pairing at five
    ticker: null,

    show: function () {
        if (!els.door.hidden) return;
        els.door.hidden = false;
        S.locked = true;
        if (!els["door-name"].value) els["door-name"].value = suggestName();
        if (!els["door-pw-name"].value) els["door-pw-name"].value = suggestName();
        this.step("ask");
        // Not on a phone: a keyboard springing up over the explanation of what is about to
        // happen means the explanation is never read.
        if (!phone()) els["door-name"].focus();
    },

    hide: function () {
        if (els.door.hidden) return;
        els.door.hidden = true;
        S.locked = false;
        this.stopClock();
        this.say("");
        render();
    },

    step: function (name) {
        els.door.dataset.step = name;
        this.say("");
        if (name !== "code") this.stopClock();
    },

    /** What went wrong, or what is about to happen. The server writes real sentences; this
     *  shows them rather than translating them into a code the reader has to look up. */
    say: function (words, calm) {
        els["door-say"].textContent = words || "";
        els["door-say"].className = "say" + (calm ? " calm" : "");
    },

    ask: function () {
        var self = this;
        var name = els["door-name"].value.trim() || suggestName();
        els["door-ask"].disabled = true;
        this.say(T.webDoorAsking, true);
        api.pair(name).then(function (d) {
            self.pairing = { id: d.pairing_id, expires: d.expires };
            self.wrong = 0;
            self.clearDigits();
            self.step("code");
            self.startClock();
            // Only ever from the fixture: a real server does not put the code in the reply, which
            // is the one thing this whole screen exists to be true about.
            if (MOCK && d.mock_code) self.say("Fixture: the code is " + d.mock_code, true);
            if (!phone()) els["door-digits"].firstElementChild.focus();
        }).catch(function (e) {
            // The one refusal that needs a sentence of our own: three requests in ten minutes is
            // the limit because each one puts an alert on somebody's screen, and "rate_limited"
            // on its own reads like a fault rather than like a door working correctly.
            self.say(e.code === "rate_limited"
                ? e.message + " " + T.webDoorRateLimited
                : e.message || T.webDoorAskFailed);
        }).then(function () { els["door-ask"].disabled = false; });
    },

    confirm: function () {
        var self = this;
        if (!this.pairing) { this.step("ask"); return; }
        var code = this.code();
        if (code.length !== 6) { this.say(T.webDoorSixDigits); return; }
        els["door-confirm"].disabled = true;
        this.say(T.webDoorChecking, true);
        api.confirmPair(this.pairing.id, code).then(function () {
            self.signedIn();
        }).catch(function (e) {
            // Only a refusal counts. A code that never reached the Mac was not a wrong guess, and
            // five dropped connections should not tell somebody their pairing is finished when the
            // far end never touched its counter.
            if (e.code === "forbidden") self.wrong += 1;
            self.clearDigits();
            // Five wrong guesses and the pending pairing is gone at the far end. Counting here
            // too is what keeps the screen honest about it: the server's refusal for a dead
            // pairing and its refusal for a wrong digit are the same 403 with the same code, and
            // only the sentence differs — which is a fine thing to *show* and a poor thing to
            // branch on.
            if (self.wrong >= 5) {
                self.pairing = null;
                self.step("ask");
                self.say(T.webDoorFinished);
                return;
            }
            self.say(e.message || T.webDoorWrongCode);
            if (!phone()) els["door-digits"].firstElementChild.focus();
        }).then(function () { els["door-confirm"].disabled = false; });
    },

    password: function () {
        var self = this;
        var secret = els["door-password"].value;
        if (!secret) { this.say(T.webDoorNeedPassword); return; }
        // Return in either box asks for this, and so does the button; the box is in a form as
        // well now. One attempt at a time whichever of them it came from.
        if (els["door-pw-go"].disabled) return;
        els["door-pw-go"].disabled = true;
        this.say(T.webDoorChecking, true);
        api.password(secret, els["door-pw-name"].value.trim() || suggestName()).then(function () {
            els["door-password"].value = "";
            self.signedIn();
        }).catch(function (e) {
            self.say(e.message || T.webDoorWrongPassword);
        }).then(function () { els["door-pw-go"].disabled = false; });
    },

    /** The cookie is set; ask the server again from the top rather than assuming. */
    signedIn: function () {
        this.pairing = null;
        this.wrong = 0;
        this.hide();
        toast(T.webDoorPaired);
        api.start();
    },

    code: function () {
        var out = "";
        var boxes = els["door-digits"].children;
        for (var i = 0; i < boxes.length; i++) out += boxes[i].value;
        return out;
    },

    clearDigits: function () {
        var boxes = els["door-digits"].children;
        for (var i = 0; i < boxes.length; i++) {
            boxes[i].value = "";
            boxes[i].classList.remove("filled");
        }
    },

    startClock: function () {
        var self = this;
        this.stopClock();
        function tick() {
            if (!self.pairing) return;
            var left = Math.max(0, self.pairing.expires - Math.floor(Date.now() / 1000));
            if (left <= 0) {
                self.pairing = null;
                self.step("ask");
                self.say(T.webDoorExpired);
                return;
            }
            var m = Math.floor(left / 60), sec = left % 60;
            els["door-left"].textContent = m + ":" + (sec < 10 ? "0" : "") + sec;
        }
        tick();
        this.ticker = setInterval(tick, 1000);
    },

    stopClock: function () { clearInterval(this.ticker); this.ticker = null; }
};

/**
 * What to call this device, guessed from the browser so that nobody has to think of a name for
 * a thing they are holding. Editable, because the guess is often "Chrome on Mac" when what
 * matters is "the work laptop".
 *
 * **Not translated, deliberately.** What comes out of here is stored on the Mac and read back in
 * the paired-devices list in Settings — a list drawn in whatever language the *Mac* is set to.
 * A name suggested in the phone's language would arrive there as a word out of place in a list
 * of names, and it is a name: the reader can type whatever they like over it.
 */
function suggestName() {
    var ua = navigator.userAgent || "";
    if (/iPhone/.test(ua)) return "iPhone";
    if (/iPad/.test(ua)) return "iPad";
    if (/Android/.test(ua)) return /Mobile/.test(ua) ? "Android phone" : "Android tablet";
    var browser = /Edg\//.test(ua) ? "Edge"
        : /OPR\//.test(ua) ? "Opera"
        : /Chrome\//.test(ua) ? "Chrome"
        : /Firefox\//.test(ua) ? "Firefox"
        : /Safari\//.test(ua) ? "Safari" : "";
    var machine = /Macintosh|Mac OS X/.test(ua) ? "Mac"
        : /Windows/.test(ua) ? "Windows"
        : /Linux/.test(ua) ? "Linux" : "";
    if (browser && machine) return browser + " on " + machine;
    return browser || machine || "A browser";
}

/* ---- the six boxes -------------------------------------------------------
   A code read off another screen and typed with a thumb. Every ordinary thing
   somebody does to a field like this works: paste the whole code anywhere in
   it, type straight through without tabbing, backspace out of an empty box.
   -------------------------------------------------------------------------- */

(function digits() {
    var boxes = Array.prototype.slice.call(els["door-digits"].children);

    function fill(from, text) {
        var digits = text.replace(/\D/g, "").split("");
        for (var i = from; i < boxes.length && digits.length; i++) {
            boxes[i].value = digits.shift();
            boxes[i].classList.add("filled");
        }
        var next = Math.min(boxes.length - 1, from + text.replace(/\D/g, "").length);
        boxes[next].focus();
        // Six digits in and there is nothing else this screen is for.
        if (Door.code().length === 6) Door.confirm();
    }

    boxes.forEach(function (box, index) {
        box.addEventListener("input", function () {
            var typed = box.value;
            box.value = "";
            fill(index, typed);
        });
        box.addEventListener("keydown", function (ev) {
            if (ev.key === "Backspace" && !box.value && index > 0) {
                ev.preventDefault();
                boxes[index - 1].value = "";
                boxes[index - 1].classList.remove("filled");
                boxes[index - 1].focus();
            } else if (ev.key === "ArrowLeft" && index > 0) {
                ev.preventDefault(); boxes[index - 1].focus();
            } else if (ev.key === "ArrowRight" && index < boxes.length - 1) {
                ev.preventDefault(); boxes[index + 1].focus();
            } else if (ev.key === "Enter") {
                ev.preventDefault(); Door.confirm();
            }
        });
        box.addEventListener("paste", function (ev) {
            var text = (ev.clipboardData || window.clipboardData).getData("text") || "";
            if (!/\d/.test(text)) return;
            ev.preventDefault();
            fill(index, text);
        });
        box.addEventListener("focus", function () { box.select(); });
    });
})();

els["door-ask"].addEventListener("click", function () { Door.ask(); });
els["door-confirm"].addEventListener("click", function () { Door.confirm(); });
els["door-restart"].addEventListener("click", function () { Door.pairing = null; Door.step("ask"); });
els["door-to-password"].addEventListener("click", function () { Door.step("password"); });
els["door-to-pair"].addEventListener("click", function () { Door.step("ask"); });
els["door-pw-go"].addEventListener("click", function () { Door.password(); });
els["door-name"].addEventListener("keydown", function (ev) { if (ev.key === "Enter") Door.ask(); });
// The password box is in a form now — see the note beside it — so Return would otherwise reload
// the page and lose whatever was typed. It means the same thing it always did.
document.getElementById("door-pw-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    Door.password();
});
els["door-password"].addEventListener("keydown", function (ev) { if (ev.key === "Enter") Door.password(); });
els["door-pw-name"].addEventListener("keydown", function (ev) { if (ev.key === "Enter") Door.password(); });


/* ==========================================================================
   6. Deriving what to show
   ========================================================================== */

var RANK = { waiting: 0, working: 1, idle: 2, unknown: 3 };

function rankOf(s) { return RANK[s.state] == null ? 9 : RANK[s.state]; }

/**
 * The order is held still while the pointer is inside the list.
 *
 * A list that sorts itself is a list that can move a row out from under a click, and the click
 * still lands — on whatever slid into its place. Holding the order costs nothing while somebody
 * is reading, and it is given up the moment the set of waiting sessions changes: a new question
 * outranks a steady list, and getting to the top is the entire point of that state.
 */
var hold = null;

function waitingKey() {
    return S.sessions.filter(function (s) { return s.state === "waiting"; })
        .map(function (s) { return s.id; }).sort().join(",");
}
function freezeOrder() {
    if (!hold) hold = { order: ordered().map(function (s) { return s.id; }), waiting: waitingKey() };
}
function thawOrder() {
    if (!hold) return;
    hold = null;
    renderList();
}

/** Waiting first, always. The list exists to answer "which one stopped and wants me", and
 *  an answer that is third from the bottom is not one. Within a state, alphabetical, so a
 *  row that has not changed does not move. */
function ordered() {
    if (hold && hold.waiting !== waitingKey()) hold = null;
    var q = S.filter.trim().toLowerCase();
    var list = S.sessions.filter(function (s) {
        if (!q) return true;
        return ((s.label || "") + " " + (s.cwd || "") + " " + (s.tty || "") + " " + (s.backend || ""))
            .toLowerCase().indexOf(q) >= 0;
    });
    if (hold) {
        var at = {};
        hold.order.forEach(function (id, i) { at[id] = i; });
        return grouped(list.sort(function (a, b) {
            var ai = at[a.id] == null ? 1e9 : at[a.id], bi = at[b.id] == null ? 1e9 : at[b.id];
            return (ai - bi) || (rankOf(a) - rankOf(b)) || (a.label || "").localeCompare(b.label || "");
        }));
    }
    return grouped(list.sort(function (a, b) {
        return (rankOf(a) - rankOf(b))
            || (a.label || "").localeCompare(b.label || "")
            || (a.id < b.id ? -1 : 1);
    }));
}

/* ---- dispatched work ----------------------------------------------------
   A task is a session the app started because another session asked it to.
   Everything here reads `S.tasks` and nothing writes it, so an app with no
   orchestrator — an empty list, or a 404 swallowed on the way in — leaves
   every function below answering exactly what it answered before the feature
   existed. That is the whole degradation story, and it is why these are
   separate functions rather than lines inside the render.
   -------------------------------------------------------------------------- */

/** Still going. The three the app spends children on; everything else is over. */
function taskLive(t) {
    return !!t && (t.state === "queued" || t.state === "spawning" || t.state === "briefed");
}

/**
 * Whether a task still gets to say where a row sits.
 *
 * A finished task does, for as long as the child's terminal is still open. The app closes that
 * tab on its own schedule (or leaves it, if told to), and while it is there it belongs under the
 * root that asked for it — a row that jumps out from under its root at the exact moment somebody
 * looks over to read the result turns "where did that go" into a scroll through the list. Once
 * the tab is gone there is no row to hold, and the task is history.
 */
function taskShaping(t) {
    if (taskLive(t)) return true;
    if (!t || !t.finishedAt || !t.child || !t.child.terminalId) return false;
    var id = t.child.terminalId;
    return S.sessions.some(function (s) { return s.id === id; });
}

/** The task a session is the child of, live or long over — what the header reads. The freshest
 *  one wins: a terminal id is reused, and the interesting task is never the older one. */
function taskOfChild(id) {
    var best = null;
    for (var i = 0; i < S.tasks.length; i++) {
        var t = S.tasks[i];
        if (!t || !t.child || t.child.terminalId !== id) continue;
        if (!best) { best = t; continue; }
        if (taskLive(t) && !taskLive(best)) { best = t; continue; }
        if (taskLive(best) && !taskLive(t)) continue;
        if ((t.created || 0) >= (best.created || 0)) best = t;
    }
    return best;
}

/** The tasks a session is the root of, of the ones still shaping the list. */
function tasksOfRoot(id) {
    return S.tasks.filter(function (t) {
        return taskShaping(t) && t.root && t.root.terminalId === id;
    });
}

/** One word for where a task got to, in the reader's language. Cancelled and timed out are
 *  filed under failed on purpose: the row has one line, and neither of them succeeded. */
function taskWord(t) {
    if (taskLive(t)) return T.webTaskRunning;
    return t && t.state === "success" ? T.webTaskDone : T.webTaskFailed;
}

/**
 * The children, pulled up under the sessions that asked for them.
 *
 * Done after the sort rather than inside it, because it is not a sort: a child sits under its
 * root wherever the root landed, whatever state the child is in. A waiting child still reaches
 * the top of the list — it is carried there by its root, which is where somebody looking for it
 * would start.
 *
 * A child whose root is not on screen — filtered out, or a session that has since closed — is
 * left exactly where the sort put it. It is still somebody's session, and hiding it or floating
 * it to the top would be this feature deciding something it was not asked to decide.
 */
function grouped(list) {
    if (!S.tasks.length || list.length < 2) return list;

    var childOf = {};                       // child row id → the root row it belongs under
    var here = {};
    list.forEach(function (s) { here[s.id] = true; });
    S.tasks.forEach(function (t) {
        if (!taskShaping(t) || !t.child || !t.root) return;
        var kid = t.child.terminalId, root = t.root.terminalId;
        if (!kid || !root || kid === root) return;
        if (!here[kid] || !here[root]) return;
        childOf[kid] = root;
    });
    // A root that is itself somebody's child cannot carry anyone: the app refuses to dispatch
    // that deep, so it means a stale record, and moving a row under a row that is itself moving
    // is how a list loses one. Depth stays one, here as well as over there.
    Object.keys(childOf).forEach(function (kid) {
        if (childOf[childOf[kid]]) delete childOf[kid];
    });
    var moved = Object.keys(childOf);
    if (!moved.length) return list;

    var kids = {};
    list.forEach(function (s) {
        var root = childOf[s.id];
        if (root) (kids[root] || (kids[root] = [])).push(s);
    });
    var out = [];
    list.forEach(function (s) {
        if (childOf[s.id]) return;
        out.push(s);
        if (kids[s.id]) kids[s.id].forEach(function (k) { out.push(k); });
    });
    // Nothing this does is worth a row going missing. If the count moved, the grouping was
    // wrong about something and the ungrouped list is the honest answer.
    return out.length === list.length ? out : list;
}

function byId(id) {
    for (var i = 0; i < S.sessions.length; i++) if (S.sessions[i].id === id) return S.sessions[i];
    return null;
}

/** What has to be the same for a transcript to still be current. The line is in it because a
 *  session that is still working is still writing. */
function revisionOf(s) { return s ? s.state + "|" + (s.line || "") + "|" + (s.label || "") : ""; }

/* ==========================================================================
   7. Render
   Every path in here is a full render from S. The list keeps its DOM nodes
   between renders — partly so a row can animate to its new place, partly so
   the selected row does not lose focus every time the stream breathes.
   ========================================================================== */

var rowNodes = {};       // id → live <li>
var leavingNodes = {};   // id → detached-from-the-list-order <li>
// An end request has already crossed the confirmation boundary. Kept outside `S.sessions`
// because it is browser-side progress rather than a fact from the stream; the stream may remove
// the session before the request that caused it has made the osascript round trip back.
var closingID = null;

var firstList = true;

function onSessions() {
    var open = S.openId ? byId(S.openId) : null;

    // Anything that has just stopped gets a pulse, and whatever is open gets refetched.
    S.sessions.forEach(function (s) {
        var was = S.seen[s.id];
        // Working to idle, and only that. A session that stops to ask a question has not
        // finished — it has started shouting, and the row is already doing that.
        if (was && was.state === "working" && s.state === "idle") {
            var node = rowNodes[s.id];
            if (node && !reduced) {
                node.classList.remove("finished");
                void node.offsetWidth;              // restart the animation rather than let it be ignored
                node.classList.add("finished");
                setTimeout(function () { node.classList.remove("finished"); }, 1600);
            }
        }
        if (open && s.id === open.id && (!was || was.rev !== revisionOf(s))) {
            loadTranscript(s.id, true);
        }
        // An agent being read has its own reason to refetch, and the session's revision cannot
        // give it: a session sitting between turns with three agents out looks unchanged the
        // whole time they work. `agentsRev` moves when any of them writes a line.
        if (S.agent && S.agent.sid === s.id) {
            var mine = agentRow(s.id, S.agent.id);
            if (mine) { S.agent.meta = mine; renderAgentHead(); }
            if (!was || was.agents !== agentsRev(s)) loadAgent(s.id, S.agent.id, true);
        }
        S.seen[s.id] = { state: s.state, rev: revisionOf(s), agents: agentsRev(s) };
    });

    // A session that has gone away takes its row, and its transcript, with it.
    Object.keys(S.seen).forEach(function (id) { if (!byId(id)) delete S.seen[id]; });
    if (S.selectedId && !byId(S.selectedId)) S.selectedId = null;
    if (S.openId && !byId(S.openId)) {
        // The stream is the stronger answer: if the row vanished while its end request is still
        // in flight, the session is already closed. Let the visible wait finish naturally and
        // make the later HTTP reply harmless instead of tearing the confirmation away mid-spin.
        if (closingID === S.openId) SessionActions.gone(S.openId);
        else closeDetail(true);
    }

    render();

    // A session that was started from this page, as soon as the list has it. Before `openWanted`
    // for the same reason it is here at all: both of them are "somebody asked for this session",
    // and this one asked most recently.
    var started = Start.check();

    // A session the URL asked for — a tapped notification — as soon as it is in the list. It is
    // tried on every list rather than once, because a cold start routes before it knows what
    // sessions exist.
    var routed = openWanted();

    // The first list to arrive puts the highlight somewhere, so the arrow keys and Return mean
    // something without a click first. On a desk it also opens that session: a two-column layout
    // whose second column is empty on arrival is a layout apologising for itself. On a phone it
    // does not — there, opening is a whole screen, and nobody asked for it yet.
    if (firstList && S.sessions.length) {
        firstList = false;
        // Not when somebody asked for a particular one. Opening the top of the list first and
        // theirs a moment later is two screens for one tap, and the first of them is wrong.
        // A Mac with nothing running is where this matters: the first list to arrive there can
        // be the one carrying the session that was just started from this page.
        if (!routed && !wantedSession && !started) {
            var top = ordered()[0];
            if (top) { if (phone()) { S.selectedId = top.id; render(); } else { openSession(top.id, true); } }
        }
        // The first list is the whole list, so a session that is not in it is gone — a
        // notification about a tab somebody has since closed. Let go of it rather than hold the
        // default open hostage to a session that is never coming.
        wantedSession = null;
    }
}

function render() {
    renderCounts();
    renderList();
    renderDetailHead();
    // After the session's header and before the composer, because it hides both of them when an
    // agent is being read, and hiding a thing that has just been drawn is cheaper than drawing
    // one that is about to be hidden.
    renderAgentHead();
    renderComposer();
    // The Session info card's buttons depend on whether the session is idle.
    if (typeof Info === "object" && Info) Info.follow();
    if (typeof StatusLine === "object" && StatusLine) StatusLine.follow();
    renderWaiting();
    renderAgents();
}

function renderConn() {
    var label = { live: T.webConnLive, connecting: T.webConnConnecting, offline: T.webConnOffline,
                  locked: T.webConnLocked,
                  retrying: fill(T.webConnRetrying, { n: S.retryIn }) }[S.conn] || S.conn;
    els.conn.dataset.state = S.conn;
    els["conn-label"].textContent = label;
    els.conn.title = S.conn === "live"
        ? T.webConnTipLive + (S.version ? " · " + S.version : "")
        : S.conn === "locked" ? T.webConnTipLocked
        : T.webConnTipDown;
}

function renderCounts() {
    var working = 0, waiting = 0, unknown = 0;
    S.sessions.forEach(function (s) {
        if (s.state === "working") working++;
        else if (s.state === "waiting") waiting++;
        else if (s.state === "unknown") unknown++;
    });
    var bits = [];
    if (working) bits.push('<span class="part">' + esc(fill(T.webCountWorking, { n: working })) + "</span>");
    if (waiting) bits.push('<span class="part waiting">' + esc(fill(T.webCountWaiting, { n: waiting })) + "</span>");
    if (unknown) bits.push('<span class="part quiet">' + esc(fill(T.webCountUnreadable, { n: unknown })) + "</span>");
    if (!bits.length) {
        var quiet = S.sessions.length
            ? fill(S.sessions.length === 1 ? T.webCountQuietOne : T.webCountQuietMany, { n: S.sessions.length })
            : T.webCountNone;
        bits.push('<span class="part quiet">' + esc(quiet) + "</span>");
    }
    els.counts.innerHTML = bits.join("");
}

/**
 * Keep a removed row where it was visibly sitting while the live rows take their final places.
 * Taking it out of flow first means the ordinary FLIP moves its neighbours all the way to their
 * destination; the old row can then leave on top without being appended to a different position
 * or remaining a keyboard and pointer target after its session is gone.
 */
function leaveRow(id, node, seen, rowsBox) {
    if (node.flipAnim) { node.flipAnim.cancel(); node.flipAnim = null; }
    if (node.enterFadeAnim) { node.enterFadeAnim.cancel(); node.enterFadeAnim = null; }
    node.classList.remove("entering", "finished");
    node.classList.add("leaving");
    node.setAttribute("aria-hidden", "true");
    node.tabIndex = -1;
    node.style.position = "absolute";
    node.style.top = (seen.top - rowsBox.top) + "px";
    node.style.left = (seen.left - rowsBox.left - seen.marginLeft) + "px";
    node.style.width = seen.width + "px";
    node.style.opacity = seen.opacity;
    node.style.transform = "none";
    leavingNodes[id] = node;

    function finish() {
        if (leavingNodes[id] !== node) return;
        if (node.parentNode) node.parentNode.removeChild(node);
        delete leavingNodes[id];
    }

    node.leaveAnim = node.animate(
        [{ opacity: seen.opacity, transform: "none" },
         { opacity: 0, transform: "translateY(-4px) scale(.97)" }],
        { duration: 190, easing: "cubic-bezier(.4, 0, 1, 1)" }
    );
    node.leaveAnim.onfinish = finish;
    node.leaveAnim.oncancel = finish;
}

function discardLeaving(id) {
    var node = leavingNodes[id];
    if (!node) return;
    delete leavingNodes[id];
    if (node.leaveAnim) node.leaveAnim.cancel();
    if (node.parentNode) node.parentNode.removeChild(node);
}

function renderList() {
    if (typeof SwipeRows === "object" && SwipeRows && (!phone() || !S.write)) SwipeRows.reset(true);
    // Nothing has ever arrived and the wait has gone on long enough to be worth drawing. No rows
    // are built: there are none to build, and the skeleton stands in for the empty state rather
    // than sitting beside it.
    if (Waits.list.visible) { drawListSkeleton(); return; }

    var list = Start.arrange(ordered());

    // FLIP, first half: both where every row is visibly sitting and where layout put it before
    // the DOM is touched. The distinction matters while a previous FLIP is still running: a
    // stream frame that does not change layout must leave that animation alone, while one that
    // does change it starts again from the visible position rather than snapping to the old end.
    var before = {};
    if (!reduced) {
        Object.keys(rowNodes).forEach(function (id) {
            var node = rowNodes[id];
            var box = node.getBoundingClientRect();
            var style = getComputedStyle(node);
            before[id] = {
                top: box.top, left: box.left, width: box.width,
                layoutTop: node.offsetTop, opacity: style.opacity,
                marginLeft: parseFloat(style.marginLeft) || 0
            };
        });
    }

    setSpinners([]);
    var wanted = {};
    list.forEach(function (s) { wanted[s.id] = true; });

    // A leaving row has to be taken out of flow before live nodes are appended into their new
    // order. Otherwise appendChild moves the obsolete node to an arbitrary slot, so it fades
    // from somewhere it was never seen and the live rows animate to positions that are already
    // out of date.
    var rowsBox = !reduced ? els.rows.getBoundingClientRect() : null;
    Object.keys(rowNodes).forEach(function (id) {
        if (wanted[id]) return;
        var node = rowNodes[id];
        delete rowNodes[id];
        if (!node.parentNode) return;
        if (reduced) node.parentNode.removeChild(node);
        else leaveRow(id, node, before[id], rowsBox);
    });

    list.forEach(function (s) {
        var node = rowNodes[s.id];
        if (!node) {
            discardLeaving(s.id);
            node = buildRow(s);
            rowNodes[s.id] = node;
            if (!reduced && !Start.arriving(s.id)) {
                node.classList.add("entering");
                setTimeout(function (n) { return function () { n.classList.remove("entering"); }; }(node), 300);
            }
        }
        fillRow(node, s);
        // appendChild moves a node that is already here, which is what makes this a reorder
        // rather than a rebuild — the element under the pointer stays the element under the pointer.
        els.rows.appendChild(node);
    });

    // Not a session and deliberately not in `rowNodes`: it cannot be selected, filtered or
    // counted. It lives in the same container solely so the real row replaces the same shape at
    // the top, instead of arriving as an unexplained reorder after the band has been spinning.
    var starting = Start.placeholder();
    if (starting) els.rows.insertBefore(starting, els.rows.firstChild);

    // FLIP, second half: start each row where it used to be and let it travel to where it now is.
    //
    // Driven by the animation API rather than by writing a transform and taking it away again.
    // This list re-renders on every breath of the stream, and pinning through inline styles means
    // a pin and its release can land in the same style recalculation — the browser is entitled to
    // see only the end of that and animate nothing, which is how rows came to sit at the position
    // they held two updates ago. An animation object cannot be coalesced away, and the one already
    // running is cancelled by name rather than by hoping it has finished.
    if (!reduced) {
        Object.keys(rowNodes).forEach(function (id) {
            var from = before[id];
            if (!from) return;
            var node = rowNodes[id];
            // The target did not move, so neither should an animation already travelling there.
            // Cancelling it on every one-second line update was the remaining way a row could
            // snap halfway through an otherwise correct reorder.
            if (from.layoutTop === node.offsetTop) return;
            // If entry and reorder overlap, preserve the opacity already reached and hand the
            // transform over to FLIP. Two animations replacing the same transform is not a
            // meaningful composite, and which one wins differs between WebKit and Chromium.
            if (node.classList.contains("entering")) {
                node.classList.remove("entering");
                node.style.opacity = from.opacity;
                node.enterFadeAnim = node.animate(
                    [{ opacity: from.opacity }, { opacity: 1 }],
                    { duration: 160, easing: "cubic-bezier(.2, .7, .2, 1)" }
                );
                node.enterFadeAnim.onfinish = function () {
                    node.style.opacity = "";
                    node.enterFadeAnim = null;
                };
            }
            if (node.flipAnim) node.flipAnim.cancel();
            // Cancellation exposes the new untransformed layout position. `from.top` was read
            // before it, so this delta resumes at the exact pixel that was on screen.
            var delta = from.top - node.getBoundingClientRect().top;
            if (Math.abs(delta) < 1) return;
            node.flipAnim = node.animate(
                [{ transform: "translateY(" + delta + "px)" }, { transform: "none" }],
                { duration: 240, easing: "cubic-bezier(.2, .7, .2, 1)" }
            );
        });
    }

    var empty = !list.length && !starting && !listUnknown();
    var homeEmpty = empty && !S.sessions.length && !S.filter;
    els["list-empty"].className = "empty" + (homeEmpty ? " home-hero-list" : "");
    els["list-empty"].hidden = !empty;
    if (empty) {
        // Four of them, and telling them apart is the whole job: nothing matches what was typed,
        // this browser was refused, there are genuinely no sessions, or nothing has arrived yet.
        // One of those is somebody's own doing and three are not.
        var says = S.sessions.length
            ? [fill(T.webEmptyFilterTitle, { q: S.filter }), T.webEmptyFilterHint]
            : (S.locked
                ? [T.webEmptyLockedTitle, T.webEmptyLockedHint]
                : S.conn === "live"
                    ? [T.noSession, T.webEmptyNoneHint]
                    : [T.webEmptyWaitTitle, T.webEmptyWaitHint]);
        els["list-empty"].innerHTML = "<b>" + esc(says[0]) + "</b>" + esc(says[1]);
    }
}

function buildRow(s) {
    var li = document.createElement("li");
    li.className = "row";
    li.setAttribute("role", "option");
    li.tabIndex = -1;
    li.innerHTML =
        '<span class="kid" hidden aria-hidden="true">└</span>' +
        '<canvas class="mark"></canvas>' +
        '<div class="title"><span class="label"></span><span class="who" hidden></span></div>' +
        '<div class="meta"><span class="path"></span><span class="tty"></span>' +
        '<span class="agents-chip" hidden><span class="dot"></span><span class="n"></span></span>' +
        '<span class="task-chip" hidden></span></div>' +
        '<div class="state"></div>' +
        '<button class="swipe-end" type="button" hidden>' + esc(T.webEndSession) + '</button>';
    li.addEventListener("click", function () {
        if (closingID === s.id) return;
        openSession(s.id);
    });
    return li;
}

function fillRow(node, s) {
    var closing = closingID === s.id;
    var closingVisible = closing && Waits.end.visible;
    var pending = Optimistic.entries(s.id).length > 0;
    node.dataset.id = s.id;
    node.dataset.state = s.state;
    if (closingVisible) node.dataset.closing = "1"; else delete node.dataset.closing;
    if (pending) node.dataset.pending = "1"; else delete node.dataset.pending;
    node.classList.toggle("selected", s.id === S.selectedId);
    node.classList.toggle("open", s.id === S.openId);
    node.setAttribute("aria-selected", s.id === S.selectedId ? "true" : "false");
    node.setAttribute("aria-disabled", closing ? "true" : "false");

    var mark = node.querySelector(".mark");
    if (!drawIcon(mark, s.icon, 4)) mark.classList.add("none"); else mark.classList.remove("none");

    var title = node.querySelector(".title");
    title.querySelector(".label").textContent = s.label || s.tty || s.id;
    // Tinted with the project's own colour, so two sessions in one project read as one project
    // before either title has been read. Waiting overrides it in CSS — that row is not about
    // which project it is.
    title.style.color = s.icon ? tint(s.icon.accent) : "";

    node.querySelector(".path").textContent = shortPath(s.cwd);
    node.querySelector(".tty").textContent = s.tty || s.backend || "";

    // Always name the assistant, exactly as the Mac list does. The project mark on the left says
    // which project; this product mark answers the independent question, Claude or Codex.
    var who = node.querySelector(".who");
    who.hidden = !ASSISTANT_LOGOS[s.assistant];
    who.innerHTML = who.hidden ? "" : assistantLogo(s.assistant) +
        "<span>" + esc(assistantName(s.assistant)) + "</span>";

    // How many agents this session has out. **A number and nothing else**, sitting with the path
    // and the tty rather than with the state: the state line answers "does this want me", and
    // background work never does. It is also the only place in the list that says anything the
    // terminal could not have been asked — an agent leaves no mark on a screen.
    var chip = node.querySelector(".agents-chip");
    var out = 0, list = s.agents || [];
    for (var a = 0; a < list.length; a++) if (list[a].state === "running") out++;
    chip.hidden = !out;
    if (out) chip.querySelector(".n").textContent = String(out);

    // Where this row sits in somebody's work: a session that was started for another one, or
    // one that did the starting. Both are drawn where the agent count is and in the same
    // register, because they answer the same kind of question — this is context about a row,
    // never a row asking for anything.
    //
    // Only the tasks still shaping the list count. A child whose task ended an hour ago is
    // just a session again, and a chip that never goes away is furniture.
    var task = S.tasks.length ? taskOfChild(s.id) : null;
    var kid = task && taskShaping(task) ? task : null;
    // The indent is a claim about the row above, so it is only drawn when that row is there.
    // The chip is a claim about this session, which is true whether or not its root is on
    // screen — a child whose root has closed is still a child, and still worth saying so.
    var under = !!(kid && kid.root && kid.root.terminalId && kid.root.terminalId !== s.id
                   && byId(kid.root.terminalId));
    var roots = S.tasks.length && !kid ? tasksOfRoot(s.id) : [];
    var mine = node.querySelector(".task-chip");
    var glyph = node.querySelector(".kid");
    if (kid) {
        if (under) node.dataset.depth = "1"; else delete node.dataset.depth;
        glyph.hidden = !under;
        mine.hidden = false;
        mine.dataset.live = taskLive(kid) ? "1" : "0";
        mine.textContent = T.webTaskChild + " · " + taskWord(kid);
        mine.title = kid.title || "";
    } else {
        delete node.dataset.depth;
        glyph.hidden = true;
        if (roots.length) {
            mine.hidden = false;
            mine.dataset.live = roots.some(taskLive) ? "1" : "0";
            mine.textContent = T.webTaskRoot + " · " + roots.length;
            mine.title = T.webTaskTasks + ": " + roots.map(function (t) { return t.title || t.id; }).join(" · ");
        } else {
            mine.hidden = true;
            mine.textContent = "";
            mine.title = "";
        }
    }

    // The state line is rebuilt only when the *shape* of it changes. A working session sends a
    // new line every second, and throwing away the spinner's canvas to write one string would
    // restart the animation once a second — which is exactly the thing that should look steady.
    var state = node.querySelector(".state");
    // A local turn is a fact about the trip to the Mac, not a replacement for the state the
    // Mac reported. Keep `data-state` untouched and give the transient trip its own shape; a
    // waiting row keeps its louder request for attention alongside the quieter delivery note.
    var shape = closingVisible ? "closing" : (pending ? "pending-" + s.state : s.state);
    if (state.dataset.shape !== shape) {
        state.dataset.shape = shape;
        if (shape === "closing") {
            state.innerHTML = '<canvas class="spin"></canvas><span class="line">' +
                esc(T.webClosing) + "</span>";
        } else if (pending) {
            state.innerHTML = (s.state === "waiting"
                ? '<span class="wants">' + esc(T.sessionWaiting) + "</span>"
                : "") + '<canvas class="spin"></canvas><span class="line">' +
                esc(T.webPending) + "</span>";
        } else if (s.state === "waiting") {
            state.innerHTML = '<span class="wants">' + esc(T.sessionWaiting) + "</span>";
        } else if (s.state === "working") {
            state.innerHTML = '<canvas class="spin"></canvas><span class="line"></span>';
        } else if (s.state === "unknown") {
            // Not silence — a screen that could not be read is a different fact from "idle",
            // and drawing it as idle would be a confident wrong answer about someone's work.
            state.innerHTML = '<span class="unread">' + esc(T.webStateUnreadable) + "</span>";
        } else {
            state.innerHTML = "";
        }
    }
    if (pending || shape === "working" || shape === "closing") {
        state.querySelector(".line").textContent = pending ? T.webPending :
            (shape === "closing" ? T.webClosing : (s.line || ""));
        var canvas = state.querySelector(".spin");
        drawSpinner(canvas, spinPhase);
        spinners.push(canvas);
    }
}

/* ---- the transcript ------------------------------------------------------ */

function renderDetailHead() {
    var s = S.openId ? byId(S.openId) : null;
    var ending = !!(s && closingID === s.id);
    els["detail-head"].dataset.closing = ending ? "on" : "off";
    els.back.disabled = ending;
    // Blank, not "No session open", while the list is still on its way — see `listUnknown`. The
    // header is the third thing that used to announce an absence and then fill in a name.
    els["detail-name"].textContent = s ? (s.label || s.tty || s.id)
        : (listUnknown() ? "" : T.webNoSessionOpen);
    els["detail-name"].style.color = s && s.icon ? tint(s.icon.accent) : "";
    var sub = [];
    if (s) {
        sub.push(shortPath(s.cwd));
        if (s.tty) sub.push(s.tty);
        if (s.state === "waiting") sub.push(T.sessionWaiting);
        else if (s.state === "working") sub.push(T.webStateWorking);
        else if (s.state === "unknown") sub.push(T.webStateUnreadable);
        // What this session was opened to do, if something opened it. Every known task and not
        // only the fresh ones: the list stops grouping an old task's row because the grouping is
        // about *where a row sits*, and a reader who has this session open has asked about this
        // session — "what was this for" has an answer long after the row has gone back to normal.
        var task = S.tasks.length ? taskOfChild(s.id) : null;
        if (task) {
            if (task.title) sub.push(task.title);
            sub.push(taskWord(task));
            var used = task.usage || {};
            if (used.total) sub.push("↓ " + agentTokens(used.total));
            // Only when there is a figure. Codex is billed by the plan rather than the token, so
            // the Mac sends null rather than a zero, and a "$0.0000" beside real work is a lie
            // that reads as a measurement.
            if (typeof used.costUsd === "number") sub.push("$" + used.costUsd.toFixed(4));
        }
    }
    els["detail-sub"].textContent = sub.join("  ·  ");
    drawIcon(els["detail-mark"], s && s.icon, 5);
    // Nothing to ask the Mac about with no session open. The chip stays on screen: a control
    // that comes and goes with the selection
    // is a header that moves under whoever is reading it.
    els["tx-refresh"].disabled = !s || ending;
    // **Hidden unless this page is being read on the Mac itself.**
    //
    // The button brings a session's terminal to the front over there. Pressed from a phone it
    // does something real and entirely invisible to the person pressing it, which is the
    // definition of a control that should not be on screen — and it is sitting in a two-chip
    // header where the session's own name is what gets truncated to make room for it.
    //
    // The test is where the page was *loaded from*, not the screen width and not whether a
    // keyboard is attached: an iPad with a Magic Keyboard is not at the Mac either, and a narrow
    // window on the Mac still is. Loopback means the browser and the app are the same machine.
    // Reaching it at a LAN address while sitting at the Mac hides a button that would have
    // worked, which is a much smaller mistake than offering one that cannot be seen to work.
    els["tx-focus"].hidden = !atMac();
    els["tx-focus"].disabled = !s || !S.write || ending;
    els["tx-focus"].title = S.write ? T.webShowOnMacTip : T.webShowOnMacOff;
    // The mark and title open the same compact action menu. The Git row is read-only, so the
    // menu remains reachable when sending is off; its mutating rows each keep their own gate.
    els["detail-focus"].disabled = !s || ending;
    els["detail-focus"].title = T.webSessionActions;
    els["detail-focus"].setAttribute("aria-label", T.webSessionActions);
    els["detail-actions-title"].disabled = !s || ending;
    els["detail-actions-title"].title = T.webSessionActions;
    els["detail-actions-title"].setAttribute("aria-label", T.webSessionActions);
    els["session-focus"].disabled = !s || !S.write || ending;
    // Reading needs no write switch; the menu it sits in already does, and the day the menu
    // opens for a read-only device this row is the one that should still work.
    els["session-info"].disabled = !s || ending;
    els["session-git-more"].disabled = !s || ending;
    els["session-git"].disabled = !s || ending;
    els["session-commit"].disabled = !s || !S.write || ending;
    els["session-push"].disabled = !s || !S.write || ending;
    els["session-end"].disabled = !s || !S.write || ending;
    if (!s) { SessionActions.close(); GitPanel.follow(); }
}

function renderTranscript() {
    var box = els.tx;
    setOptimisticSpinners([]);
    // Blank rather than the home screen while the list is still on its way — see `listUnknown`.
    // A pane that says "pick a session" and then opens one on its own is a pane that changed its
    // mind in front of the reader; a pane that is briefly empty is a pane that is loading.
    var home = !S.openId && !listUnknown();
    box.classList.toggle("home", home);
    els["tx-scroll"].classList.toggle("home", home);
    if (!S.openId) {
        box.innerHTML = !home ? "" :
            '<section class="home-hero" aria-labelledby="home-hero-title">' +
            '<div class="copy"><span class="rule" aria-hidden="true"></span>' +
            '<h1 id="home-hero-title">' + esc(T.webNoSessionOpen) + '</h1>' +
            '<p>' + esc(T.webPickSession) + '</p></div></section>';
        return;
    }
    // The speaker belongs to the open session. Keeping this beside the transcript render means a
    // Codex answer cannot inherit the page's historical Claude default while sessions switch.
    var session = byId(S.openId);
    WHO.assistant = assistantName(session && session.assistant);
    // One pane, one of two conversations in it: the session's, or one of the agents it sent
    // away. Everything below this line is the same either way — the same blocks, the same folds,
    // the same reading order — because an agent's transcript is the same kind of record.
    var view = S.agent || S.tx;
    // The merge is for drawing only. A quiet fetch replaces `S.tx`, while this separate tail
    // survives until that fetch contains the corresponding real entry. Agent files never get a
    // tail: they are read-only conversations and have no composer to originate one.
    var viewEntries = view.entries.slice();
    if (!S.agent) viewEntries = viewEntries.concat(Optimistic.entries(S.openId));
    if (view.error && !viewEntries.length) { box.innerHTML = '<div class="tx-note err">' + esc(view.error) + "</div>"; return; }
    var transcriptNotice = view.error
        ? '<div class="tx-note err">' + esc(view.error) + "</div>"
        : "";
    if (view.loading && !viewEntries.length) {
        // Nothing at all for the first breath, and then the skeleton — see `Waits`. A pane that
        // shows a placeholder for eighty milliseconds and takes it away again reads as a fault.
        box.innerHTML = Waits.tx.visible ? txSkeleton() : "";
        return;
    }
    if (!viewEntries.length) {
        // An agent with nothing in its file is not the same absence as a session with nothing in
        // its file, and it is common: the first second of every agent's life looks like this.
        box.innerHTML = '<div class="tx-note">' + esc(S.agent ? T.agentEmpty : T.noOutput) + "</div>";
        return;
    }

    // Built a block at a time rather than an entry at a time, the way `Transcript.render` builds
    // it: one message, or one whole run of tool calls, per block. Reversing is then the order the
    // blocks go out in, and a run stays one thing whichever way round the transcript is read.
    var entries = viewEntries.filter(worthDrawing);
    var blocks = [];
    var liveRun = null;    // the last run of tool calls, and where its block landed
    var liveAt = -1;
    var i = 0;
    while (i < entries.length) {
        if (entries[i].role !== "tool") { blocks.push(entryHTML(entries[i])); i += 1; continue; }
        // A question is its own block and breaks the run around it. Everything else a tool does
        // is machinery that folds away; this one is a sentence somebody was asked, and it is the
        // only thing in the pane that was ever addressed to the reader.
        if (askOf(entries[i])) { blocks.push(askHTML(entries[i])); i += 1; continue; }
        var run = [];
        while (i < entries.length && entries[i].role === "tool" && !askOf(entries[i])) {
            run.push(entries[i]); i += 1;
        }
        liveRun = run; liveAt = blocks.length;
        blocks.push(runHTML(run));
    }
    // The run at the end of a session that is still working is the one happening right now: its
    // count is still climbing and the call at the end of it has not come back yet. Drawn a
    // second time with the sweep on it, so the part of the page that is moving is the part of
    // the work that is moving. It has to be the *last* block — a run with an answer written
    // under it is finished, whatever the session is doing now.
    if (liveRun && liveAt === blocks.length - 1 && transcriptWorking()) {
        blocks[liveAt] = runHTML(liveRun, true);
    }
    if (S.newestFirst) blocks.reverse();
    box.innerHTML = transcriptNotice + blocks.join("");
    var pending = box.querySelectorAll(".entry.pending canvas.spin");
    for (var p = 0; p < pending.length; p++) {
        drawSpinner(pending[p], spinPhase);
        optimisticSpinners.push(pending[p]);
    }
    // Said out loud so the keyboard-bar guard above can put the new buttons back out of the tab
    // order. A custom event rather than a direct call: this function is the transcript's, and
    // what somebody else needs to do afterwards is their business, not a line in here.
    document.dispatchEvent(new CustomEvent("clawdline:rendered"));
}

/**
 * Whether what is on screen is still being written — the session's own state while its
 * transcript is open, and the agent's while one of its agents is being read. It is the whole
 * test behind the sweep on the last run: a transcript that has stopped has nothing in flight,
 * however recently it stopped.
 */
function transcriptWorking() {
    if (S.agent) return !!(S.agent.meta && S.agent.meta.state === "running");
    var s = byId(S.openId);
    return !!(s && s.state === "working");
}

/**
 * Whether an entry is worth a place on the screen at all.
 *
 * The wire files a tool call and the first line of what it returned under the same role, and
 * that first line is whatever the tool happened to print first. For a grep over Swift that is
 * regularly `///` — a comment marker, matched, returned, and drawn here as a turn of its own
 * with a timestamp beside it. It is not a small thing to skip: on a phone it is a whole block,
 * indistinguishable at a glance from something somebody said.
 *
 * The test is deliberately narrow. **Anything with a letter or a digit in it stays**, so a result
 * of `0`, `nil` or `[]` — all of which mean something — is drawn. What goes is punctuation on
 * its own, which cannot mean anything without the line it was cut from.
 */
function worthDrawing(e) {
    if (!e) return false;
    if (e.role !== "tool") return true;      // prose is never dropped, whatever it says
    if (e.tool) return true;                 // a call is a step, even one with no arguments
    var text = String(e.text == null ? "" : e.text).trim();
    return text.length > 0 && /[\p{L}\p{N}]/u.test(text);
}

/**
 * A run of consecutive tool entries, folded the way the bar folds one.
 *
 * The machinery is what makes a transcript unreadable: a single answer can sit under thirty
 * lines of paths and shell, and the two sentences worth reading are somewhere past them. So a
 * run comes back as one line saying how much it stands for, and opens when it is asked to.
 *
 * Which run is which comes from `Transcript.render`, not from a rule invented here: the wire
 * files a tool call and what it returned under the same role, and a run is every one of them in
 * a row. Only the calls are counted, because a call is the step — the one entry with a name on
 * it. Two of them is where folding starts to pay: a line reading "1 step" is longer than the
 * step it hid.
 *
 * **The run at the end folds like any other, and that is a change.** The bar exempts it, on the
 * grounds that it is the one still happening — which is right on a Mac, where the pane is wide
 * and the exemption costs two lines. On a phone it is the entire complaint: while an agent is
 * working, every new call lands in the tail, so the tail *is* the screen, and what a reader gets
 * is six greps where the sentence they were reading used to be. A count that climbs says the
 * same thing about a live run and says it in one line — and the names beside it say what kind of
 * work it is, which thirty wrapped command lines never quite manage to.
 */
function runHTML(run, live) {
    var names = [];
    run.forEach(function (e) { if (e.tool) names.push(e.tool); });
    var key = foldKey(run);
    var last = run.length - 1;
    // `live` marks the one call that has not come back yet, which is the last one in the run.
    // Written out rather than `run.map(toolRowHTML)`, which would hand `map`'s index in as the
    // flag and light every row up.
    function rows(lit) {
        return run.map(function (e, n) { return toolRowHTML(e, lit && n === last); }).join("");
    }
    if (names.length < 2) return rows(live);
    var open = !!S.expanded[key];
    // Folded, the pill is the only thing standing for the call in flight, so the sweep goes on
    // the pill; opened, it goes on the row that is actually running.
    return foldHTML(key, names, open, live && !open) + (open ? rows(live) : "");
}

/* --------------------------------------------------------------------------
   A question Claude stopped to ask
   ------------------------------------------------------------------------ */

/**
 * The questions in an entry, or null when it is an ordinary tool call.
 *
 * A marked entry that will not parse comes back as an **empty array, not null** — which still
 * routes it into the block below. That is the whole point of a marker: whatever else goes
 * wrong, a control character followed by JSON is never put on somebody's screen.
 *
 * Remembered on the entry, because this runs for every entry on every render and the answer is a
 * property of text that does not change.
 */
function askOf(e) {
    if (!e || e.role !== "tool" || !e.tool) return null;
    if (e.ask === undefined) e.ask = parseAsk(e.text);
    return e.ask;
}

function parseAsk(text) {
    var s = String(text == null ? "" : text);
    if (s.slice(0, ASK_MARK.length) !== ASK_MARK) return null;
    var rows = null;
    try { rows = JSON.parse(s.slice(ASK_MARK.length)); } catch (err) { rows = null; }
    if (!rows || !rows.length) return [];

    var out = [];
    for (var i = 0; i < rows.length; i++) {
        var row = rows[i] || {};
        var options = [];
        var list = row.o && row.o.length ? row.o : [];
        for (var j = 0; j < list.length; j++) {
            var o = list[j] || {};
            if (typeof o.l !== "string" || !o.l) continue;
            options.push({ label: o.l, note: typeof o.d === "string" ? o.d : "" });
        }
        out.push({
            header: typeof row.h === "string" ? row.h : "",
            text: typeof row.q === "string" ? row.q : "",
            multi: row.m === true,
            options: options
        });
    }
    return out;
}

/**
 * A question, drawn in full and never folded.
 *
 * **Every option gets a row of its own.** The whole complaint this answers is that a decision
 * somebody was asked to make was invisible on a phone — folded into a run of tool calls with
 * nothing but the word `AskUserQuestion` showing — and a folded question is the same bug with a
 * nicer border on it. So there is no fold key here and no handle: it is open because it is the
 * one thing in the pane addressed to the person reading it.
 *
 * The numbers are the terminal's own. Option 3 here is what `3` selects in the picker on the
 * Mac, which is worth having in front of somebody who is about to walk over to it.
 *
 * Nothing here is pressable, and that is deliberate rather than unfinished: see the note above
 * `renderWaiting` for what a press would have to send and why this page cannot send it.
 */
function askHTML(e) {
    var questions = askOf(e) || [];
    var body = questions.map(function (q) {
        var head = "";
        if (q.header) head += '<div class="askhead">' + esc(q.header) + "</div>";
        if (q.multi) head += '<div class="askany">' + esc(T.webAskAny) + "</div>";
        var asked = q.text ? '<div class="askq">' + inlineMd(q.text) + "</div>" : "";
        var options = q.options.map(function (o, n) {
            return '<li class="askopt"><span class="n">' + (n + 1) + "</span>" +
                '<span class="what"><b>' + inlineMd(o.label) + "</b>" +
                (o.note ? '<span class="note">' + inlineMd(o.note) + "</span>" : "") +
                "</span></li>";
        }).join("");
        return head + asked + (options ? '<ol class="askopts">' + options + "</ol>" : "");
    }).join("");

    return '<div class="entry ask" data-role="ask">' +
        whoHTML("assistant", e.at) +
        '<div class="body"><div class="askbox">' +
        '<div class="asktag">' + esc(T.webAskLabel) + "</div>" + body +
        "</div></div></div>";
}

/**
 * One tool call, or one thing a tool said back, as a single line that opens when it is pressed.
 *
 * A call's arguments are a shell command, a path, a regular expression — text with no sentence in
 * it, written to be run rather than read. Drawn as prose it wraps to four lines and takes a
 * quarter of a phone screen to say "it ran a grep". So the first line of it goes in a row that
 * cannot wrap, with the tool's name in front, and the whole of it is one press away.
 *
 * The key is the entry's own content hash with a letter in front, so it cannot collide with the
 * key of the run it sits inside — `S.expanded` is one map and both live in it.
 */
function toolRowHTML(e, live) {
    var key = "e" + foldKey([e]);
    var open = !!S.expanded[key];
    var head = e.tool
        ? '<span class="toolname">' + esc(e.tool) + "</span>"
        : '<span class="caret">' + (open ? "⏷" : "⏵") + "</span>";
    // No timestamp on these. A time beside every grep is a column of numbers nobody reads, and
    // the turn above it is already stamped.
    return '<div class="entry toolrow" data-role="tool"' + (live ? ' data-live="1"' : "") + ">" +
        '<div class="who">' + esc(WHO.tool) + "</div>" +
        '<div class="body">' +
        '<button type="button" class="toolline" data-fold="' + key +
        '" aria-expanded="' + (open ? "true" : "false") + '">' +
        head + '<span class="subject">' + esc(firstLine(e.text)) + "</span></button>" +
        (open ? '<div class="toolbody">' + richText(e.text) + "</div>" : "") +
        "</div></div>";
}

/** The first line of something, with its runs of whitespace closed up. What is left is what fits
 *  on one row; the rest of the truncating is the browser's, which knows how wide the row is. */
function firstLine(text) {
    var line = String(text == null ? "" : text).split("\n")[0].replace(/\s+/g, " ").trim();
    return line || "…";
}

/**
 * The line a folded run leaves behind, and the handle that opens it.
 *
 * A `<button>` rather than a styled `<div>`: the rest of this page can be driven from the
 * keyboard and this may not be the one thing that cannot. It stays put once the run is open —
 * the bar draws no handle over an opened run, but its own click handler toggles both ways, and
 * a control that deletes itself on use leaves the focus standing on nothing.
 */
function foldHTML(key, names, open, live) {
    return '<div class="entry folded" data-role="tool"' + (live ? ' data-live="1"' : "") + ">" +
        '<div class="who">' + esc(WHO.tool) + "</div>" +
        '<div class="body"><button type="button" class="pill" data-fold="' + key +
        '" aria-expanded="' + (open ? "true" : "false") + '">' +
        '<span class="caret">' + (open ? "⏷" : "⏵") + "</span>" +
        // The count is its own element so it cannot be broken across two lines by the names
        // beside it: a pill reading "9" above "steps" is the one part of this that has to be
        // legible at a glance.
        '<span class="steps">' + esc(fill(T.webSteps, { n: names.length })) + "</span>" +
        (open ? "" : '<span class="what">' + esc(distinct(names).join(" · ")) + "</span>") +
        "</button></div></div>";
}

/** Tool names in the order they first ran, without repeats — five greps read as one thing. */
function distinct(names) {
    var seen = {};
    return names.filter(function (n) {
        if (seen[n]) return false;
        seen[n] = true;
        return true;
    });
}

/**
 * Identifies a folded run so the reader's choice to open it survives a re-render.
 *
 * Content-derived rather than positional, for the reason `Transcript.foldKey` gives: this pane
 * re-renders every time the session moves, and an index would slide under the reader and open a
 * different run than the one they clicked. FNV-1a, as there — 32-bit rather than 64 because
 * JavaScript has no 64-bit integer, and the key only has to tell one run in a transcript from
 * the handful of others in the same transcript.
 */
function foldKey(run) {
    var hash = 0x811c9dc5;
    run.forEach(function (e) {
        // The separator is the one the bar puts there, so a name cannot run into the text
        // beside it and hash the same as a different pair that happens to join up the same
        // way. Written as an escape: a raw control character in a source file is something
        // an editor will quietly eat.
        var text = (e.tool || "") + "\u0001" + (e.text || "");
        for (var i = 0; i < text.length; i++) {
            hash = Math.imul(hash ^ text.charCodeAt(i), 0x01000193);
        }
    });
    return (hash >>> 0).toString(36);
}

function toggleFold(key) {
    if (S.expanded[key]) delete S.expanded[key];
    else S.expanded[key] = true;
    renderTranscript();
    // The row the reader pressed was just thrown away and written again, so the focus has to be
    // put back on its replacement — otherwise opening a run with the keyboard is also the moment
    // the keyboard loses its place in the page.
    var again = els.tx.querySelector('[data-fold="' + key + '"]');
    if (again) again.focus({ preventScroll: true });
}

els.tx.addEventListener("click", function (ev) {
    var handle = ev.target.closest ? ev.target.closest("[data-fold]") : null;
    if (handle) toggleFold(handle.getAttribute("data-fold"));
});

els.tx.addEventListener("keydown", function (ev) {
    if (ev.key !== "Enter" && ev.key !== " ") return;
    if (!ev.target.closest || !ev.target.closest("[data-fold]")) return;
    // Return means "open the selected session" everywhere else on this page, and the handler
    // that does it calls preventDefault — which would stop the button ever seeing the click the
    // browser was about to make out of this key. While a fold has the focus, the key is its own.
    ev.stopPropagation();
});

// Two of these three are copy and come from the server before the first render; the middle one
// is a name, and a name is the same word in fourteen languages. See `paintStatic`.
var WHO = { user: "you", assistant: "claude", tool: "tool" };

/** A transcript speaker, with the current assistant's mark when this browser opted into it. */
function whoHTML(role, at) {
    var mark = "";
    if (role === "assistant" && S.assistantIcons) {
        var session = byId(S.openId);
        mark = assistantLogo(session && session.assistant);
    }
    return '<div class="who"><span class="speaker">' + mark + esc(WHO[role]) + "</span>" +
        (at ? '<time data-at="' + at + '">' + esc(clockOf(at)) + "</time>" : "") + "</div>";
}

function entryHTML(e) {
    var role = WHO[e.role] ? e.role : "assistant";
    var body = (e.tool ? '<span class="toolname">' + esc(e.tool) + "</span>" : "") + richText(e.text);
    if (e.pending) {
        var attached = e.imageCount
            ? '<div class="pending-images">' + esc(fill(e.imageCount === 1
                ? T.webAttachedImage : T.webAttachedImages, { n: e.imageCount })) + "</div>"
            : "";
        body += attached + '<div class="pending-state" role="status"><canvas class="spin"></canvas><span>' +
            esc(T.webPending) + "</span></div>";
    }
    return '<div class="entry' + (e.pending ? ' pending' : '') + '" data-role="' + role + '">' +
        whoHTML(role, e.at) +
        '<div class="body">' + body + "</div></div>";
}

/* --------------------------------------------------------------------------
   Markdown, to the same depth the bar's own pane reads it
   ------------------------------------------------------------------------ */

/**
 * Enough Markdown to read a Claude Code answer, and no more.
 *
 * The recognisers are ported from ``Markdown.swift`` so the two surfaces agree about what a
 * table is and what a heading is — a document that draws as a table on the Mac and as a row of
 * pipes here would mean one of them is lying about the same text. Its rule about failure is
 * ported too: **anything unrecognised falls through as plain text.** A stray asterisk on screen
 * is a blemish; a sentence that disappeared into a parser is a bug you cannot see.
 *
 * Everything is escaped before a single tag is written. This text came off somebody's terminal,
 * and a terminal will carry any angle bracket you put in front of it.
 */
function richText(text) {
    if (!text) return "";
    var lines = String(text).replace(/\r\n?/g, "\n").split("\n");
    var out = [];
    var i = 0;

    while (i < lines.length) {
        var line = lines[i];
        var trimmed = line.trim();

        // Fenced code. The opening line may carry a language tag, which is not content.
        if (trimmed.indexOf("```") === 0) {
            var body = [];
            i += 1;
            while (i < lines.length && lines[i].trim().indexOf("```") !== 0) { body.push(lines[i]); i += 1; }
            i += 1;
            out.push('<pre class="code">' + esc(body.join("\n")) + "</pre>");
            continue;
        }

        if (!trimmed) { i += 1; continue; }

        if (isRule(trimmed)) { out.push("<hr>"); i += 1; continue; }

        var head = heading(trimmed);
        if (head) {
            // h1 and h2 in a transcript entry would outweigh the page's own furniture, so the
            // levels are shifted down: what matters is that six of them stay distinguishable.
            var tag = "h" + Math.min(6, head[0] + 2);
            out.push("<" + tag + ">" + inlineMd(head[1]) + "</" + tag + ">");
            i += 1;
            continue;
        }

        // A table is a run of piped lines *under a separator row*. Detected as a block rather
        // than line by line, or the row of dashes lands in the document as a paragraph.
        if (looksLikeTableRow(trimmed) && i + 1 < lines.length && isTableSeparator(lines[i + 1].trim())) {
            var rows = [];
            while (i < lines.length && looksLikeTableRow(lines[i].trim())) { rows.push(lines[i].trim()); i += 1; }
            out.push(tableHTML(rows));
            continue;
        }

        if (trimmed.charAt(0) === ">") {
            var quoted = [];
            while (i < lines.length && lines[i].trim().charAt(0) === ">") {
                quoted.push(lines[i].trim().slice(1).trim());
                i += 1;
            }
            out.push("<blockquote>" + inlineMd(quoted.join(" ")) + "</blockquote>");
            continue;
        }

        if (listItem(line)) {
            var block = [];
            while (i < lines.length && (listItem(lines[i]) || (block.length && lines[i].trim() && !isBlockStart(lines[i])))) {
                block.push(lines[i]);
                i += 1;
            }
            out.push(listHTML(block));
            continue;
        }

        // Paragraph: gather until something else starts. Single newlines inside one are a wrapped
        // line and not a break, which is how the source was written and how the bar reads it.
        var paragraph = [trimmed];
        i += 1;
        while (i < lines.length) {
            var next = lines[i].trim();
            if (!next || isBlockStart(lines[i])) break;
            paragraph.push(next);
            i += 1;
        }
        out.push("<p>" + inlineMd(paragraph.join(" ")) + "</p>");
    }
    return out.join("");
}

function isBlockStart(line) {
    var t = line.trim();
    return !t || t.indexOf("```") === 0 || t.charAt(0) === ">" || !!heading(t)
        || !!listItem(line) || isRule(t) || looksLikeTableRow(t);
}

function heading(line) {
    var m = /^(#{1,6})\s+(.*)$/.exec(line);
    return m ? [m[1].length, m[2].trim()] : null;
}

/** The marker and the text, plus how far it is indented — the indent is what makes a nested
 *  list nested, and it is the one thing the bar's flat list cannot show. */
function listItem(line) {
    var m = /^(\s*)([-*+])\s+(.*)$/.exec(line);
    if (m) return { indent: m[1].length, ordered: false, marker: "", text: m[3] };
    m = /^(\s*)(\d{1,3})\.\s+(.*)$/.exec(line);
    if (m) return { indent: m[1].length, ordered: true, marker: m[2], text: m[3] };
    return null;
}

function isRule(line) {
    return /^(-{3,}|_{3,}|\*{3,})$/.test(line);
}

function looksLikeTableRow(line) {
    return line.indexOf("|") >= 0 && (line.match(/\|/g) || []).length >= 2;
}

function isTableSeparator(line) {
    return looksLikeTableRow(line) && /^[|\-: ]+$/.test(line);
}

/** The cells of one row. A leading and a trailing pipe are borders, not empty cells. */
function tableCells(row) {
    var parts = row.split("|");
    if (!parts[0].trim()) parts.shift();
    if (parts.length && !parts[parts.length - 1].trim()) parts.pop();
    return parts.map(function (c) { return c.trim(); });
}

/**
 * A real `<table>`, which is the whole reason this page can do better than the bar.
 *
 * The pane on the Mac had to build one out of `NSTextTable` after two simpler attempts failed —
 * pipes left in and set in monospace do not line up, because a CJK glyph comes from a fallback
 * face whose advance is not reliably twice the monospace one. A browser has actual table layout,
 * so the columns are the browser's problem and the alignment markers can be honoured on top.
 *
 * Wide tables scroll inside their own box. A table is the one thing in a transcript that can be
 * genuinely wider than the reading column, and the alternative — a page that scrolls sideways —
 * breaks every other line on the screen to rescue one.
 */
function tableHTML(rows) {
    var separator = -1;
    for (var r = 0; r < rows.length; r++) { if (isTableSeparator(rows[r])) { separator = r; break; } }
    var align = separator >= 0 ? tableCells(rows[separator]).map(function (spec) {
        var left = spec.charAt(0) === ":", right = spec.charAt(spec.length - 1) === ":";
        return left && right ? "center" : right ? "right" : left ? "left" : "";
    }) : [];

    var grid = rows.filter(function (row) { return !isTableSeparator(row); }).map(tableCells);
    if (!grid.length) return "";
    var columns = 0;
    grid.forEach(function (row) { columns = Math.max(columns, row.length); });
    if (!columns) return "";

    function cell(tag, value, c) {
        var style = align[c] ? ' style="text-align:' + align[c] + '"' : "";
        return "<" + tag + style + ">" + inlineMd(value || "") + "</" + tag + ">";
    }
    var html = '<div class="tablewrap"><table>';
    // A header row only when the separator says so — the first row of a table with no separator
    // above it is data, and promoting it would be inventing a heading.
    var start = 0;
    if (separator === 1) {
        html += "<thead><tr>";
        for (var c = 0; c < columns; c++) html += cell("th", grid[0][c], c);
        html += "</tr></thead>";
        start = 1;
    }
    html += "<tbody>";
    for (var g = start; g < grid.length; g++) {
        html += "<tr>";
        for (var k = 0; k < columns; k++) html += cell("td", grid[g][k], k);
        html += "</tr>";
    }
    return html + "</tbody></table></div>";
}

/** A run of list lines, nested by how far each one is indented. */
function listHTML(block) {
    var items = [];
    block.forEach(function (line) {
        var item = listItem(line);
        if (item) { items.push(item); return; }
        // A wrapped continuation line belongs to the item above it.
        if (items.length) items[items.length - 1].text += " " + line.trim();
    });
    if (!items.length) return "";

    var html = "";
    var stack = [];            // the indents currently open, outermost first
    items.forEach(function (item) {
        while (stack.length && item.indent < stack[stack.length - 1].indent) {
            html += "</li></" + stack.pop().tag + ">";
        }
        if (!stack.length || item.indent > stack[stack.length - 1].indent) {
            var tag = item.ordered ? "ol" : "ul";
            if (stack.length) html += "<" + tag + ">";       // opens inside the item above
            else html += "<" + tag + ">";
            stack.push({ indent: item.indent, tag: tag });
        } else {
            html += "</li>";
        }
        html += "<li>" + inlineMd(item.text);
    });
    while (stack.length) html += "</li></" + stack.pop().tag + ">";
    return html;
}

/**
 * `**bold**`, `*italic*`, `_italic_`, `` `code` ``, `~~struck~~`, `[text](url)`.
 *
 * Unmatched markers stay as text, the same rule the bar follows. The one thing this does that
 * the bar does not have to think about is *where a link may point*: a transcript is somebody
 * else's output, so only the three schemes that cannot execute anything are turned into links.
 * Absolute file paths are a fourth recognised shape, but are labels rather than anchors: they
 * name a file on the Mac, which a phone browser cannot open.
 */
function inlineMd(text) {
    var s = esc(text);

    // Code spans come out of the string first and go back in last.
    //
    // Splitting on backticks and running the emphasis rules over each piece separately looks
    // equivalent to this and is not. In `**turn on `remote_write` first**` the opening `**` and
    // the closing `**` land in different pieces, so neither one matches, and the reader is shown
    // literal asterisks around the exact sentence somebody was trying to make emphatic — which
    // is how a real answer came to read `**在 Cloudflare Zero Trust 幫 `host` 掛一層 Access**`.
    //
    // The stand-in is `<0>`, `<1>`, …, and the angle brackets are what make it safe: `esc` above
    // has already turned every `<` in the source into `&lt;`, so the only ones left in the string
    // are the ones put here, and nothing the source can say will collide with them. It is also
    // inert to every rule below, carrying no `*`, `_`, `~`, `[` or `(` for them to match on.
    // What goes back is the span's escaped text and nothing else: whatever was inside a code
    // span was never markup, and never met a rule that could have read it as any.
    var spans = [];
    s = s.replace(/`([^`]*)`/g, function (all, code) {
        // An unclosed backtick is a backtick, not the start of code that never ends: this needs
        // a closing one to match, so a lone backtick is left standing where it was written.
        spans.push(code);
        return "<" + (spans.length - 1) + ">";
    });

    s = s
        // Written links and bare ones in **one pass, alternating**, and the order inside the
        // pattern is the whole trick: at each position the `[label](href)` form is tried first,
        // so it is consumed whole and the bare rule never sees the URL inside it. Two passes
        // cannot do this — autolinking afterwards would match the `href="…"` the first pass just
        // produced and nest an anchor inside an anchor, and autolinking first would eat the
        // target out of every written link before the markdown rule could read it.
        //
        // Safe because `esc` has already run: what is matched here is escaped text, and what is
        // put in the attribute is the same escaped text, so a `"` in the source is `&quot;` and
        // cannot close the attribute.
        .replace(/\[([^\]\n]+)\]\(([^)\s]+)\)|(\bhttps?:\/\/[^\s<]+)/g,
                 function (all, label, href, bare) {
            if (!bare) {
                return safeHref(href)
                    ? '<a href="' + href + '" target="_blank" rel="noopener noreferrer">' + label + "</a>"
                    : localFileHref(href)
                        ? '<span class="local-ref" title="' + href + '">' + label + "</span>"
                    : all;
            }
            // Punctuation that ends a sentence is not part of the address. A closing bracket is,
            // but only if the URL opened one — which is what people writing about Wikipedia keep
            // discovering. Anything trimmed goes back outside the link, where it was meant to be.
            var url = bare, tail = "";
            while (url.length) {
                var last = url.charAt(url.length - 1);
                if (")]}".indexOf(last) >= 0) {
                    var open = "([{".charAt(")]}".indexOf(last));
                    // `>=`, not `>`: one opened and one closed is balanced, and the closer
                    // belongs to the address. `>` chopped the tail off `…/Foo_(bar)` — the
                    // exact case this check was written for.
                    if (url.split(open).length >= url.split(last).length) break;
                } else if (".,;:!?'\u201d\u2019&".indexOf(last) < 0) {
                    break;
                }
                // A quote at the end is punctuation around the address, not part of it — and by
                // this point `esc` has turned it into an entity, so it is six characters.
                if (/&quot;$/.test(url)) { tail = "&quot;" + tail; url = url.slice(0, -6); continue; }
                // `&amp;` and friends end in `;`, and chopping the `;` off leaves `&amp` on the
                // screen — so an entity at the end is left whole.
                if (last === ";" && /&[a-z]+;$/i.test(url)) break;
                tail = last + tail;
                url = url.slice(0, -1);
            }
            if (!safeHref(url)) return all;
            return '<a href="' + url + '" target="_blank" rel="noopener noreferrer">' + url + "</a>" + tail;
        })
        // Lazy, and not "no marker inside": the bar recurses into what it finds between two
        // markers, so `**a *b* c**` is bold with an italic in it there. Refusing the whole thing
        // over the inner pair would put three pairs of asterisks back on the screen.
        .replace(/\*\*([^\n]+?)\*\*/g, "<strong>$1</strong>")
        .replace(/~~([^\n]+?)~~/g, "<del>$1</del>")
        // A single `*` or `_`, on the bar's terms: it has to open a word — start of the line, or
        // after a space or a bracket — and what it closes must not end in a space. `snake_case`
        // and `rm *.o *.a` are far more common in this text than emphasis is, and both of them
        // come through as themselves.
        .replace(/(^|[\s(])\*([^*\n]*[^\s*\n])\*/g, "$1<em>$2</em>")
        .replace(/(^|[\s(])_([^_\n]*[^\s_\n])_/g, "$1<em>$2</em>");

    return s.replace(/<(\d+)>/g, function (all, n) { return "<code>" + spans[n] + "</code>"; });
}

/** Only the schemes that cannot run anything. `javascript:` in a transcript is not a link. */
function safeHref(href) {
    return /^(https?:\/\/|mailto:)/i.test(href.replace(/&amp;/g, "&"));
}

/** A file on the Mac, not a route on this web app. Ordinary root-relative URLs stay unrecognised. */
function localFileHref(href) {
    var value = href.replace(/&amp;/g, "&");
    return /^\/(Users|private|tmp|var|Volumes|Applications|Library|opt|usr|home)\//.test(value)
        || /^file:\/\/\//i.test(value);
}

function renderComposer() {
    var on = S.write && !!S.openId && closingID !== S.openId;
    var session = S.openId ? byId(S.openId) : null;
    var placeholder = T.placeholder;
    if (session && session.assistant === "codex") {
        placeholder = placeholder.replace("Claude Code", "Codex").replace("Claude", "Codex");
        if (placeholder === T.placeholder) placeholder = "Codex…";
    }
    els.msg.setAttribute("data-placeholder", placeholder);
    els.msg.setAttribute("aria-label", placeholder);
    els["skill-menu"].setAttribute("aria-label",
        (session && session.assistant === "codex" ? "Codex" : "Claude Code") + " skills");
    els.composer.dataset.write = S.write ? "on" : "off";
    els.composer.dataset.sending = sending ? "on" : "off";
    els.composer.dataset.closing = closingID === S.openId ? "on" : "off";
    // Editable only when there is somewhere for the words to go. Not switched off while a send
    // is in flight, though — see the `beforeinput` guard below: taking the editability away from
    // a focused element takes the focus with it, and on a phone that shuts the keyboard between
    // every message.
    els.msg.contentEditable = on ? "plaintext-only" : "false";
    // What Return does differs by machine, and the soft keyboard's own key should say which.
    els.msg.setAttribute("enterkeyhint", hasKeyboard() ? "send" : "enter");
    // A picture on its own is a message. The server takes text, images, or both, and refuses
    // only the one that is neither — so the button follows the same rule.
    els.send.disabled = !on || sending || Shots.busy() || (!msgText() && !Shots.count());
    // **The width is pinned before the word changes.** "Send" and "Sending…" are different
    // lengths in every language, and swapping them mid-press moved the button, which moved the
    // box under it — so the press that started a send also made the thing you pressed jump. The
    // width is measured while it says the shorter word and held while it says the longer one,
    // rather than guessed at with a `min-width` that would be wrong in thirteen languages.
    if (sending) {
        if (sendWidth) els.send.style.minWidth = sendWidth + "px";
        els.send.textContent = T.webSending;
    } else {
        els.send.style.minWidth = "";
        els.send.textContent = T.webSend;
        // Re-measured when the words themselves change, which happens once, when `/v1/strings`
        // lands and the page stops speaking its built-in English.
        if (sendMeasured !== T.webSend) { sendMeasured = T.webSend; sendWidth = 0; }
        if (!sendWidth) sendWidth = els.send.offsetWidth;
    }
    // Return means two different things on the two kinds of machine, so the explanation is only
    // offered where there is a Return to explain. It is hover text and not a row of the screen:
    // it was a permanent line under the box once, and a permanent line to teach something once
    // is what a tooltip is for.
    els.send.title = hasKeyboard() ? T.webSendTip : "";
    els.attach.disabled = !on || sending;
    // Only ever the reason the box will not take what you type. There used to be a line here
    // saying that Return sends and Shift-Return starts a line, and it was true and it was in the
    // way: a row of every screen, forever, to teach something once. When there is nothing to
    // explain this is empty, and an empty one is not drawn at all.
    els.why.innerHTML = S.write
        ? (S.openId ? "" : esc(T.webWriteOpen))
        : words(T.webWriteOff);
}

/**
 * The line above the composer when the open session has stopped to ask something.
 *
 * **What this page can and cannot do about a question, established by trying it.**
 *
 * Claude Code's picker answers to one raw byte: the digit of the option, sent *outside* a
 * bracketed paste, selects and confirms it in one press — and in a multi-select it toggles the
 * row, with Tab then `1` to submit. `ITerm.keystroke` and `Tmux.keystroke` already send exactly
 * that byte, and the bar on the Mac already uses them for Ctrl-V.
 *
 * There is no route to them. The only write this page has is `POST /v1/sessions/<id>/send`,
 * which wraps what it is given in a bracketed paste and follows it with a Return — and a picker
 * **swallows the paste and acts on the Return**. Measured, twice, against a real session: with
 * the caret parked on the third option, sending the word "Tea" answered "Water". So the composer
 * is not a way to type an answer; it is a way to confirm a highlighted row without meaning to,
 * which is the whole reason this notice is loud rather than tidy.
 *
 * Nothing here pretends otherwise. There are no option buttons on this page, because the only
 * press they could make is that one — and a wrong answer to "shall I force-push?" costs more
 * than a missing button. What is offered instead is the true thing: the session is waiting, and
 * here is the way to put it in front of you.
 */
var waitingDrawn = null;

/* An answer that has been sent, and the session that has not caught up with it yet.
 *
 * Pressing an option closes the picker on the Mac before the next reading notices, so for about
 * a second the session is still `waiting` with no menu left to read — and the box would fall back
 * to the two sentences that say answering is only possible on the Mac. Somebody who has just
 * answered from the phone is then told their press did nothing, which is the opposite of what
 * happened. So the options stay on screen, dead, under the line that says the answer went. */
var answeredMenu = null;
var liveDrawn = null;
var agentsDrawn = null;
/// The button's resting width, and the words it was measured against.
var sendWidth = 0;
var sendMeasured = null;

function renderWaiting() {
    var box = els.waiting;
    if (!box) return;
    var open = S.openId ? byId(S.openId) : null;
    // The options, when the Mac managed to read them. When it did not — a dialog drawn in a
    // shape the parser does not know — the two sentences underneath are what this box has always
    // said, and they are still the honest answer for that case.
    var menu = open && open.state === "waiting" && open.menu ? open.menu : null;
    var rows = menu && menu.options && menu.options.length ? menu.options : null;
    // Given up after ten seconds: if the session is still waiting by then this was not the
    // answer's own gap, and the honest fallback is better than a dead menu nobody can use.
    if (answeredMenu && (!open || answeredMenu.id !== open.id || open.state !== "waiting"
                         || Date.now() - answeredMenu.at > 10000)) {
        answeredMenu = null;
    }
    var sent = !rows && !!answeredMenu;
    if (sent) rows = answeredMenu.rows;
    var want = !open || open.state !== "waiting" ? "" :
        '<div class="title">' + esc(T.webWaitingTitle) + "</div>" +
        (rows
            ? '<div class="say">' + words(sent ? T.webMenuSent : T.webMenuSay) + "</div>"
              + menuHTML(rows, sent)
            : '<div class="say">' + words(T.webWaitingSay) + "</div>" +
              '<div class="say">' + words(T.webWaitingSend) + "</div>") +
        '<button type="button" class="go" data-focus="1">' + esc(T.webShowOnMac) + "</button>";
    // The live line, from the same reading, and **above the early return below** — that return
    // exists because the *notice* rarely changes, and while a session is working the notice is
    // empty every single time, so anything after it ran once and then never again. This line
    // changes about once a second; it is the one thing here that has to be redrawn.
    //
    // Mutually exclusive with the notice by construction: a session is working or it is waiting,
    // never both, so the two never stack.
    var live = els.live;
    if (live) {
        var say = open && open.state === "working" && open.line ? open.line : "";
        if (say !== liveDrawn) {
            liveDrawn = say;
            live.textContent = "";
            setLiveSpin(null);
            if (say) {
                // The same mark the list draws, on the same clock. It replaced a dot that
                // breathed between full and quarter opacity — which said "still going" only to
                // somebody already watching it, and said nothing at a glance.
                var d = document.createElement("canvas"); d.className = "spin";
                var t = document.createElement("span"); t.className = "said";
                t.textContent = say;
                live.appendChild(d); live.appendChild(t);
                setLiveSpin(d);
                drawSpinner(d, spinPhase);
            }
            live.hidden = !say;
        }
    }

    // Written only when it has changed. `render` runs on every beat of the stream, and throwing
    // this away and building it again once a second would take the focus off the button in it
    // every time somebody was about to press it.
    if (want === waitingDrawn) return;
    waitingDrawn = want;
    box.innerHTML = want;
    box.hidden = !want;

    if (!want) return;
    // A new button, drawn while somebody may be writing. Same reason the transcript says it:
    // the keyboard-bar guard has to put it back out of the tab order.
    document.dispatchEvent(new CustomEvent("clawdline:rendered"));
}

/**
 * A menu's options, as buttons.
 *
 * **`n` is the keystroke, not the position.** It is drawn as the number the row carries and sent
 * as that same number, because Claude Code's picker acts on the digit — renumbering these to run
 * 1..n would produce buttons whose label and effect disagree, which is the failure this whole
 * feature exists to stop somebody hitting from another room.
 *
 * A row is offered only when a keystroke can reach it *and* this device may send. Otherwise it is
 * drawn and disabled: the question is still worth reading when you cannot answer it, and a button
 * that looks live and does nothing is worse than a line of text.
 */
function menuHTML(rows, spent) {
    return '<div class="menu">' + rows.map(function (o) {
        var can = !spent && o.can !== false && S.write;
        return '<button type="button" class="opt" data-key="' + esc(String(o.n)) + '"' +
            ' data-can="' + (o.can === false ? "0" : "1") + '"' +
            (o.selected ? ' data-here="1"' : "") + (can ? "" : " disabled") + ">" +
            '<span class="n">' + esc(String(o.n)) + "</span>" +
            '<span class="what">' + esc(o.label || "") +
            (o.selected ? '<span class="here">' + esc(T.webMenuHighlighted) + "</span>" : "") +
            "</span></button>";
    }).join("") + "</div>";
}

els.waiting.addEventListener("click", function (ev) {
    if (!ev.target.closest) return;

    var opt = ev.target.closest("[data-key]");
    if (opt) {
        if (!S.openId || opt.disabled) return;
        // Every option goes dead on the first press, not just the one pressed. They are answers
        // to one question: a second tap while the first is in flight would arrive as a stray
        // keystroke in whatever the session moved on to.
        Array.prototype.forEach.call(els.waiting.querySelectorAll(".opt"), function (b) {
            b.disabled = true;
        });
        // Held so the next render has something to draw when the picker has already gone.
        var asked = byId(S.openId);
        if (asked && asked.menu && asked.menu.options && asked.menu.options.length) {
            answeredMenu = { id: S.openId, rows: asked.menu.options, at: Date.now() };
        }
        api.key(S.openId, opt.dataset.key)
            .then(function () { toast(T.webMenuSent); })
            .catch(function (e) {
                toast(e.message, true);
                // Drawn again from scratch, and the cache has to be cleared to allow it: the
                // markup has not changed, so the guard in `renderWaiting` would keep the dead
                // buttons on screen and leave nothing to press.
                answeredMenu = null;
                waitingDrawn = null;
                renderWaiting();
            });
        return;
    }

    if (!ev.target.closest("[data-focus]")) return;
    if (!S.openId) return;
    api.focus(S.openId).then(function () { toast(T.webShowOnMacAsked); })
        .catch(function (e) { toast(e.message, true); });
});

/**
 * What this session has working somewhere that is not a screen.
 *
 * **The quietest thing in the composer, on purpose.** An agent running is the answer to "why has
 * this been busy for four minutes" — it is context, and it never wants anybody. The one state on
 * this page allowed to be loud is a session waiting for an answer, and a stack of bright rows
 * under it would take that away.
 *
 * A finished agent stays for a few minutes with what it came back with, then goes. The record is
 * the transcript; this is the notice.
 */
function renderAgents() {
    var box = els.agents;
    if (!box) return;
    var open = S.openId ? byId(S.openId) : null;
    var list = (open && open.agents) || [];
    var running = 0;
    for (var i = 0; i < list.length; i++) if (list[i].state === "running") running++;
    var here = S.agent ? S.agent.id : "";

    var want = !list.length ? "" :
        '<div class="head"><span>' + esc(T.webAgents) + "</span>" +
        (running ? '<span class="n">' + esc(fill(T.webAgentsCount, { n: running })) + "</span>" : "") +
        "</div>" +
        // The root, and a row like the others because it is also the way back out of one of
        // them. `main` is the terminal's own word for the conversation they all hang under, and
        // it is not translated for the same reason `general-purpose` is not: it is the name of a
        // thing in Claude Code, not a sentence about it.
        agentRowHTML({ id: "", type: "main", what: open ? (open.label || open.tty || "") : "",
                       root: true }, here) +
        list.map(function (a) { return agentRowHTML(a, here); }).join("");

    if (want === agentsDrawn) return;
    agentsDrawn = want;
    box.innerHTML = want;
    box.hidden = !want;
}

/**
 * One row of that tree.
 *
 * A button, because every row leads somewhere: an agent's row into its conversation, and the
 * root back out to the session's. The id rides on the element rather than in a closure, so a
 * repaint — which happens every time any agent reaches for a tool — rebinds nothing.
 */
function agentRowHTML(a, here) {
    var root = !!a.root;
    var said = a.state === "done" ? T.webAgentDone
             : a.state === "failed" ? T.webAgentFailed : "";
    // Running: the tool it last reached for. Finished: what it handed back. Same slot, because
    // they are the same question asked at two different times.
    var trailing = root ? "" : (a.state === "running" ? (a.doing || "") : (a.result || ""));
    var current = (a.id || "") === here;
    return '<button class="one' + (root ? " root" : "") + '" type="button"' +
        ' data-agent="' + esc(a.id || "") + '"' +
        (a.state ? ' data-state="' + esc(a.state) + '"' : "") +
        ' aria-current="' + (current ? "true" : "false") + '"' +
        (root ? "" : ' title="' + esc(T.webAgentOpen) + '"') + ">" +
        '<span class="mark"></span>' +
        '<span class="kind">' + esc(a.type || "") + "</span>" +
        '<span class="what">' + esc(a.what || "") + "</span>" +
        (trailing ? '<span class="doing">' + esc(trailing) + "</span>" : "") +
        (said ? '<span class="said">' + esc(said) + "</span>" : "") +
        "</button>";
}

/* ---- the words that live in the markup ----------------------------------- */

/**
 * Everything written into the HTML above, written again in the language that came back.
 *
 * The English is left in the document rather than blanked out, because a page whose strings
 * never arrived has to still be a page somebody can use — so the markup is the fallback and this
 * is the correction, and it runs once, before the first render, rather than on every draw.
 * Nothing below here is redrawn later: none of it changes while the page is open.
 */
function paintStatic() {
    function text(el, s) { if (el) el.textContent = s; }
    function attr(el, name, s) { if (el) el.setAttribute(name, s); }

    els.filter.placeholder = T.webFilterPlaceholder;
    attr(els.filter, "aria-label", T.webFilterLabel);
    attr(els.rows, "aria-label", T.webListLabel);
    text(els["ptr-label"], T.webPull);

    // The chevron is drawn, not typed: it points the way back whichever language is beside it.
    text(els.back, "‹ " + T.webBack);
    attr(els.back, "aria-label", T.webBackLabel);
    text(els["tx-refresh-label"], T.webGitRefresh);
    attr(els["tx-refresh"], "title", T.webGitRefresh);
    text(els["tx-focus-label"], T.webShowOnMac);
    attr(els["session-actions"], "aria-label", T.webSessionActions);
    text(els["session-focus"], T.webShowOnMac);
    text(els["session-info"], T.webSessionInfo);
    text(els["session-actions-back"], "‹ " + T.webSessionActions);
    text(els["info-title"], T.webInfoTitle);
    attr(els["info-sheet"], "aria-label", T.webInfoTitle);
    text(els["info-refresh"], T.webInfoRefresh);
    text(els["info-close"], T.webClose);
    text(els["session-git"], T.webSessionGit);
    text(els["session-end"], T.webEndSession);
    text(els["git-title"], T.webGitTitle);
    text(els["git-refresh"], T.webGitRefresh);
    text(els["git-close"], T.webGitClose);
    text(els["action-confirm-cancel"], T.webCancel);
    text(els["action-confirm-go"], T.webConfirm);

    text(els["stale-say"], T.webStale);
    text(els["stale-go"], T.webStaleGo);
    if (els["stale-shut"]) els["stale-shut"].setAttribute("aria-label", T.webClose);

    attr(els.attach, "aria-label", T.webAttach);
    attr(els.attach, "title", T.webAttach);
    els.msg.setAttribute("data-placeholder", T.placeholder);
    attr(els.msg, "aria-label", T.placeholder);
    // The send button's words and its hover text belong to `renderComposer`: both of them change
    // while a message is in flight, and one owner for a thing that moves.

    // The wordmark, and the sheet behind it.
    attr(els.brand, "aria-label", T.webSettings);
    attr(els.brand, "title", T.webSettings);
    attr(els["settings-sheet"], "aria-label", T.webSettings);
    text(els["settings-title"], T.webSettings);
    text(els["settings-notify-title"], T.webSettingsNotify);
    text(els["settings-assistant-icons-title"], T.webSettingsAssistantIcons);
    els["settings-assistant-icons-say"].innerHTML = words(T.webSettingsAssistantIconsSay);
    text(els["settings-assistant-icons-label"], T.webSettingsAssistantIconsShow);
    text(els["settings-order-title"], T.webSettingsOrder);
    els["settings-order-say"].innerHTML = words(T.webSettingsOrderSay);
    attr(els["settings-order"], "title", T.webOrderTip);
    text(els["settings-close"], T.webClose);

    // Starting one. The button carries the short words as its hover text and the long ones as
    // its name, because what a screen reader reads out is the only label a drawn `+` has.
    attr(els["start-go"], "title", T.webStart);
    attr(els["start-go"], "aria-label", T.webStartLabel);
    attr(els["start-sheet"], "aria-label", T.webStartLabel);
    text(els["start-title"], T.webStart);
    els["start-filter"].placeholder = T.webStartFilter;
    attr(els["start-filter"], "aria-label", T.webStartFilter);
    text(els["start-close"], T.webClose);
    // The × is a shape, not a word; the word is what it is called.
    attr(els["starting-close"], "aria-label", T.webClose);

    // The transcript's left margin. Claude's own name is not in the strings and is not
    // translated — it is a name, and it is the same name in fourteen languages.
    WHO.user = T.webWhoYou;
    WHO.tool = T.webWhoTool;

    // The shortcuts card. The keys themselves stay as they are — a symbol printed on a keyboard
    // is not copy — and only the sentences beside them come from the server. `esc` is on the key
    // as well, so it is left with the rest of them.
    var sheet = els.keys.querySelector(".sheet");
    attr(sheet, "aria-label", T.webKeysLabel);
    text(sheet.querySelector("h2"), T.webKeysTitle);
    var rows = [
        [["↑", "↓"], T.webKeysMove], [["⏎"], T.webKeysOpen], [["/"], T.webKeysFilter],
        [["esc"], T.webKeysEscape], [["⌘K"], T.webKeysList], [["⌘J"], T.webKeysPane],
        [["⌘I"], T.webSessionInfo],
        [["g", "G"], T.webKeysEnds], [["r"], T.webKeysReverse], [["?"], T.webKeysThis]
    ];
    sheet.querySelector("dl").innerHTML = rows.map(function (row) {
        return "<dt>" + row[0].map(function (key) { return "<kbd>" + esc(key) + "</kbd>"; }).join(" ") +
            "</dt><dd>" + words(row[1]) + "</dd>";
    }).join("");
    text(sheet.querySelector(".foot"), T.webKeysFoot);

    // The door, all three steps of it — including the two nobody sees until they need them.
    var card = els.door.querySelector(".door-card");
    attr(card, "aria-label", T.webDoorLabel);

    var ask = card.querySelector('section[data-step="ask"]');
    text(ask.querySelector(".lede"), T.webDoorAskLede);
    ask.querySelector(".fine").innerHTML = words(T.webDoorAskFine);
    text(ask.querySelector("label"), T.webDoorName);
    text(els["door-ask"], T.webDoorAsk);
    text(els["door-to-password"], T.webDoorToPassword);

    var code = card.querySelector('section[data-step="code"]');
    text(code.querySelector(".lede"), T.webDoorCodeLede);
    // A clock in the middle of a sentence, rewritten every second. So the sentence is rebuilt
    // *around* the span rather than over it: two text nodes and the element the clock owns,
    // which keeps working wherever a translation puts the hole.
    var fine = code.querySelector(".fine");
    var halves = String(T.webDoorCodeFine).split("{left}");
    fine.textContent = "";
    fine.appendChild(document.createTextNode(halves[0]));
    fine.appendChild(els["door-left"]);
    fine.appendChild(document.createTextNode(halves.length > 1 ? halves[1] : ""));
    text(els["door-left"], T.webDoorTwoMinutes);
    var boxes = els["door-digits"].children;
    for (var i = 0; i < boxes.length; i++) {
        attr(boxes[i], "aria-label", fill(T.webDoorDigit, { n: i + 1 }));
    }
    text(els["door-confirm"], T.webDoorConfirm);
    text(els["door-restart"], T.webDoorRestart);

    var pw = card.querySelector('section[data-step="password"]');
    text(pw.querySelector(".lede"), T.webDoorPasswordLede);
    text(pw.querySelector(".fine"), T.webDoorPasswordFine);
    var labels = pw.querySelectorAll("label");
    text(labels[0], T.webDoorPassword);
    text(labels[1], T.webDoorName);
    text(els["door-pw-go"], T.webDoorPasswordGo);
    text(els["door-to-pair"], T.webDoorToPair);
}

/* ---- waiting on the network ---------------------------------------------- */

/**
 * A wait, drawn only when it is a wait worth drawing.
 *
 * Two numbers, and both of them are about not making a glitch out of a fast answer:
 *
 * - **Nothing for the first 150ms.** A transcript that is already on this machine comes back
 *   inside that, and a placeholder that arrives and leaves again in a tenth of a second is read
 *   as a fault rather than as progress — the eye catches a change on screen and then finds
 *   nothing that explains it. Below the threshold the pane simply stays empty, which is what it
 *   was doing before and is over before anybody has looked at it.
 * - **320ms once it is up.** Under about a third of a second a change on screen is a flicker
 *   rather than a state. It costs nothing on the fast path, because the skeleton was never shown
 *   there; the worst case is an answer that arrives at 160ms being held back until 470ms, and
 *   that is the price of it never strobing. Paid on the slow path only, which is the one this is
 *   for in the first place.
 */
function Waiting(onShow, showAfter, minShown) {
    var SHOW_AFTER = typeof showAfter === "number" ? showAfter : 150;
    var MIN_SHOWN = typeof minShown === "number" ? minShown : 320;
    return {
        visible: false,
        shown: 0,
        timer: null,

        start: function () {
            var self = this;
            if (this.visible || this.timer) return;
            this.timer = setTimeout(function () {
                self.timer = null;
                self.visible = true;
                self.shown = Date.now();
                onShow();
            }, SHOW_AFTER);
        },

        /** The answer is in. Take the skeleton down — but not before it has been up long enough
         *  to have been a state — and then draw whatever really goes there. */
        settle: function (then) {
            var self = this;
            clearTimeout(this.timer);
            this.timer = null;
            function finish() { self.visible = false; if (then) then(); }
            if (!this.visible) { finish(); return; }
            var left = MIN_SHOWN - (Date.now() - this.shown);
            if (left <= 0) { finish(); return; }
            setTimeout(finish, left);
        }
    };
}

/**
 * Whether the page still has no idea what sessions there are.
 *
 * Two screens say "there is nothing here" — the list's empty state and the transcript's home
 * screen — and on a reload both of them were true for a moment and then wrong. Nothing had
 * arrived yet, so the list was empty because it is declared empty, and no session was open
 * because the fragment naming one cannot be honoured until that session exists. Each drew its
 * sentence, and each took it away again a few frames later: two flashes on every load.
 *
 * The wait is the test rather than a clock of its own. While it is running — the skeleton up, or
 * the 150ms before it — nobody has said, and neither screen has anything true to draw. The
 * moment it settles, whatever it settles into is the answer: a list, an empty Mac, or a
 * connection that gave up, and all three of those have words already written for them.
 */
function listUnknown() {
    return !S.arrived && (Waits.list.visible || !!Waits.list.timer);
}

var Waits = {
    tx: Waiting(function () { renderTranscript(); }),
    list: Waiting(function () { renderList(); }),
    startPress: Waiting(function () { Start.sync(); }),
    end: Waiting(function () { render(); ActionConfirm.sync(); })
};

/**
 * Browser-local turns that have crossed the HTTP boundary but not appeared in the Mac's file.
 *
 * Matching is one-to-one and forward-looking. An identical sentence from five minutes before
 * this send must not make the new one look delivered; a transcript timestamp a few seconds
 * before the browser's clock is allowed because the two machines are the same machine but the
 * request and repaint do not land on the same tick. The ten-minute upper bound is also the
 * lifetime of an echo: after that, keeping a faded promise on screen forever is less honest than
 * letting the next transcript fetch speak for itself.
 */
var Optimistic = {
    add: function (id, text, imageCount, known) {
        var entry = {
            role: "user", text: text, at: Math.floor(Date.now() / 1000),
            pending: true, imageCount: imageCount || 0, token: uuid(), wait: null,
            known: known || this.known(S.tx.id === id ? S.tx.entries : [])
        };
        entry.wait = Waiting(function () { Optimistic.expire(id, entry.token); }, 10 * 60 * 1000, 0);
        (optimisticBySession[id] || (optimisticBySession[id] = [])).push(entry);
        entry.wait.start();
        renderList();
        return entry;
    },

    entries: function (id) { return optimisticBySession[id] || []; },

    key: function (entry) {
        return String(entry.at || 0) + "\u0001" + String(entry.text == null ? "" : entry.text);
    },

    known: function (entries) {
        var self = this, found = {};
        entries.forEach(function (entry) {
            if (!entry || entry.role !== "user") return;
            var key = self.key(entry);
            found[key] = (found[key] || 0) + 1;
        });
        return found;
    },

    matches: function (pending, actual) {
        if (!actual || actual.role !== "user") return false;
        var at = Number(actual.at || 0);
        if (!at || at < pending.at - 10 || at > pending.at + 10 * 60) return false;
        var text = String(actual.text == null ? "" : actual.text);
        if (!pending.imageCount) return text === pending.text;
        var marks = text.match(/\[Image #\d+\]/g) || [];
        if (marks.length !== pending.imageCount) return false;
        return text.replace(/\[Image #\d+\]\s*/g, "").trim() === pending.text;
    },

    reconcile: function (id, actual) {
        var pending = optimisticBySession[id];
        if (!pending || !pending.length) return false;
        var used = {}, kept = [], matched = false;
        for (var i = 0; i < pending.length; i++) {
            var found = -1;
            for (var j = 0; j < actual.length; j++) {
                if (used[j] || !this.matches(pending[i], actual[j])) continue;
                var key = this.key(actual[j]), occurrence = 0;
                for (var k = 0; k <= j; k++) if (this.key(actual[k]) === key) occurrence += 1;
                if ((pending[i].known[key] || 0) >= occurrence) continue;
                found = j;
                break;
            }
            if (found < 0) { kept.push(pending[i]); continue; }
            used[found] = true;
            matched = true;
            // A later identical local turn must not claim this same real occurrence on the next
            // fetch, after the earlier pending turn has already left the container.
            var matchedKey = this.key(actual[found]), matchedOccurrence = 0;
            for (var n = 0; n <= found; n++) if (this.key(actual[n]) === matchedKey) matchedOccurrence += 1;
            for (var q = i + 1; q < pending.length; q++) {
                pending[q].known[matchedKey] = Math.max(pending[q].known[matchedKey] || 0, matchedOccurrence);
            }
            pending[i].wait.settle();
        }
        if (kept.length) optimisticBySession[id] = kept;
        else delete optimisticBySession[id];
        if (matched) renderList();
        return matched;
    },

    clear: function (id) {
        var pending = optimisticBySession[id] || [];
        for (var i = 0; i < pending.length; i++) pending[i].wait.settle();
        delete optimisticBySession[id];
    },

    clearAll: function () {
        var self = this;
        Object.keys(optimisticBySession).forEach(function (id) { self.clear(id); });
    },

    expire: function (id, token) {
        var pending = optimisticBySession[id] || [];
        var kept = pending.filter(function (entry) { return entry.token !== token; });
        if (kept.length === pending.length) return;
        if (kept.length) optimisticBySession[id] = kept;
        else delete optimisticBySession[id];
        renderList();
        if (S.openId === id && !S.agent) renderTranscript();
    }
};

/** Bars of uneven length, in the transcript's own grid, so the real entries land where these
 *  were rather than somewhere near them. The widths are fixed rather than random: a skeleton
 *  that reshuffles itself between two sessions is a second thing moving on screen. */
function txSkeleton() {
    var shapes = [[88, 54], [96, 78, 43], [71], [92, 61, 34]];
    return '<div class="skel" role="status" aria-label="' + esc(T.webReading) + '">' +
        shapes.map(function (widths) {
            return '<div class="entry skel-entry"><div class="who"><span class="bar"></span></div>' +
                '<div class="body">' + widths.map(function (w) {
                    return '<span class="bar" style="width:' + w + '%"></span>';
                }).join("") + "</div></div>";
        }).join("") + "</div>";
}

/** The same idea for the list, in the shape of the rows about to replace it. It goes into the
 *  empty state's own element: the rows below it are keyed by session id, and a placeholder with
 *  no session behind it has no business in that collection. */
function drawListSkeleton() {
    var widths = [[64, 41], [78, 52], [49, 37], [71, 45]];
    els["list-empty"].className = "skel";
    els["list-empty"].hidden = false;
    els["list-empty"].innerHTML = '<div role="status" aria-label="' + esc(T.webLoading) + '">' +
        widths.map(function (pair) {
            return '<div class="skel-row"><span class="bar mark"></span>' +
                '<span class="bar line" style="width:' + pair[0] + '%"></span>' +
                '<span class="bar sub" style="width:' + pair[1] + '%"></span></div>';
        }).join("") + "</div>";
}

/* ==========================================================================
   8. Opening a session
   ========================================================================== */

// Only the newest request may paint the transcript. Opening a session and receiving its stream
// update can start two reads almost together; without a ticket, the older snapshot can arrive
// last and erase the final entry that the newer one had already drawn.
var transcriptTicket = 0;

function loadTranscript(id, quiet) {
    var ticket = ++transcriptTicket;
    if (!quiet) {
        S.tx = { id: id, entries: [], signature: null, loading: true, error: null };
        // Only the loud kind waits visibly. A refetch behind a transcript that is already on
        // screen has nothing to stand in for — the reader is reading the last version of it.
        Waits.tx.start();
        renderTranscript();
    }
    // Returned, so a control that started this can wait for it — see the Refresh chip. Every
    // other caller ignores it, which is what a fire-and-forget refetch should look like.
    return api.transcript(id).then(function (d) {
        // A later request owns both the result and the visible wait. Settling an older request
        // here would take down the skeleton while the request that superseded it is still out.
        if (S.openId !== id || ticket !== transcriptTicket) return;
        var received = d.entries || [];
        // Reconcile before trusting the signature. The common first fetch after a send quite
        // correctly says the file is unchanged; that must preserve the echo, while an eventual
        // matching entry must retire it even if an older server reports a stale signature.
        var reconciled = Optimistic.reconcile(id, received);
        // The signature is the server's own answer to "is this the same transcript". Trusting it
        // is what keeps a refetch from throwing the reader's scroll position away every few seconds.
        if (d.signature && d.signature === S.tx.signature) {
            S.tx.loading = false;
            if (reconciled) S.tx.entries = received;
            Waits.tx.settle(renderTranscript);
            return;
        }
        var stick = atBottom();
        S.tx = { id: id, entries: received, signature: d.signature || null, loading: false, error: null };
        Waits.tx.settle(function () {
            renderTranscript();
            if (stick) toBottom();
        });
    }).catch(function (e) {
        if (S.openId !== id || ticket !== transcriptTicket) return;
        S.tx = { id: id, entries: [], signature: null, loading: false, error: e.message || T.webTranscriptFailed };
        Waits.tx.settle(renderTranscript);
    });
}

function atBottom() {
    var el = els["tx-scroll"];
    return el.scrollTop + el.clientHeight >= el.scrollHeight - 40;
}
function toBottom() {
    var el = els["tx-scroll"];
    el.scrollTop = el.scrollHeight;
}

function openSession(id, keepFocus, forceRefresh) {
    var s = byId(id);
    if (!s || closingID === id) return;
    S.selectedId = id;
    if (S.openId !== id) {
        SessionActions.close();
        ActionConfirm.close();
        // An agent belongs to the session that sent it away. Carrying one over into the next
        // session would leave somebody reading one session's background work under another
        // session's name, which is the one thing this pane must never do.
        closeAgent(true);
        S.openId = id;
        // Which runs were open is where a reader had got to in that transcript, not a setting.
        // Fold keys come from content and so would not collide across sessions, but carrying
        // them over means arriving in a new transcript with something already open.
        S.expanded = {};
        // And a picture picked for one session is not a picture for the next one.
        Shots.clear();
        Info.follow();
        GitPanel.follow();
        loadTranscript(id, false);
    } else if (forceRefresh) loadTranscript(id, true);
    if (phone()) {
        els.app.dataset.view = "detail";
        // The phone's own back gesture should mean what it looks like it means.
        try { history.pushState({ view: "detail", id: id }, ""); } catch (e) { }
    } else if (!S.paneOpen) {
        S.paneOpen = true;
        els.app.dataset.pane = "on";
    }
    render();
    SkillPicker.changed();
    if (!keepFocus && !phone()) {
        var node = rowNodes[id];
        if (node) node.focus({ preventScroll: true });
    }
}

function closeDetail(silent) {
    // The confirmation owns this session until its one-way request settles. In particular, a
    // phone back gesture must not uncover a writable-looking list while the same session is
    // still closing underneath it; the successful end clears `closingID` before coming here.
    if (closingID && S.openId === closingID) return;
    ActionConfirm.close();
    S.openId = null;
    S.agent = null;
    S.tx = { id: null, entries: [], signature: null, loading: false, error: null };
    S.expanded = {};
    Shots.clear();
    Info.follow();
    GitPanel.follow();
    SkillPicker.close();
    if (phone()) els.app.dataset.view = "list";
    renderTranscript();
    if (!silent) render();
}

/* ==========================================================================
   Reading one background agent
   The strip in the composer says a session has work happening somewhere with no screen. This is
   the rest of that sentence: the agent's own conversation, in the pane the session's was in.

   **The session does not close to show it.** `S.openId` stays exactly where it was, so the
   stream keeps the row live, the list keeps its place, and coming back is putting `S.agent`
   down rather than opening anything again. What changes is which header is on screen and which
   list of entries the transcript draws.

   The composer goes away with the header, and that is honesty rather than a shortcut: there is
   nothing to type at an agent. It was given its instructions when it was spawned and it is not
   listening for more. A box that took a sentence and had nowhere to send it would be worse than
   no box at all.
   ========================================================================== */

/** The strip's own row for an agent, which is what the header reads while a fetch is in flight. */
function agentRow(sid, agentId) {
    var s = byId(sid);
    var list = (s && s.agents) || [];
    for (var i = 0; i < list.length; i++) if (list[i].id === agentId) return list[i];
    return null;
}

/**
 * Everything about a session's agents that would change what is on screen.
 *
 * `at` is the mtime of an agent's own transcript, so this moves every time one writes a line —
 * which is the signal this pane needs and the session's own revision cannot give it: a session
 * sitting still with three agents out reports no change at all while all three are working.
 */
function agentsRev(s) {
    return ((s && s.agents) || []).map(function (a) {
        return a.id + ":" + a.state + ":" + (a.at || "") + ":" + (a.doing || "");
    }).join("|");
}

function openAgent(agentId) {
    var sid = S.openId;
    if (!sid || !agentId) return;
    if (S.agent && S.agent.id === agentId) return;
    S.agent = {
        sid: sid, id: agentId, meta: agentRow(sid, agentId),
        entries: [], signature: null, loading: true, error: null
    };
    // Which runs a reader had opened is a place in the session's transcript, not in this one.
    S.expanded = {};
    SkillPicker.close();
    loadAgent(sid, agentId, false);
    // A phone's back gesture already means "out of the thing I just opened", and this is a thing
    // that was just opened. Without this it would mean "out of the session" from in here, which
    // undoes a step the reader never asked to undo.
    if (phone()) {
        try { history.pushState({ view: "agent", id: sid, agent: agentId }, ""); } catch (e) { }
    }
    render();
}

function closeAgent(silent) {
    if (!S.agent) return;
    S.agent = null;
    S.expanded = {};
    if (silent) return;
    // The session's own entries never left `S.tx`, so going back is a repaint and not a fetch.
    renderTranscript();
    render();
}

function loadAgent(sid, agentId, quiet) {
    if (!quiet) { Waits.tx.start(); renderTranscript(); }
    api.agent(sid, agentId).then(function (d) {
        var a = S.agent;
        if (!a || a.id !== agentId || a.sid !== sid) { Waits.tx.settle(); return; }
        a.loading = false;
        if (d.agent) a.meta = d.agent;
        // The same bargain the session's transcript strikes: the server's signature answers "is
        // this still the same file", and trusting it is what stops a refetch behind a reader
        // from throwing their scroll position away every second.
        if (d.signature && d.signature === a.signature) {
            Waits.tx.settle(renderAgentHead);
            return;
        }
        var stick = atBottom();
        a.entries = d.entries || [];
        a.signature = d.signature || null;
        Waits.tx.settle(function () {
            renderAgentHead();
            renderTranscript();
            if (stick) toBottom();
        });
    }).catch(function (e) {
        var a = S.agent;
        if (!a || a.id !== agentId || a.sid !== sid) { Waits.tx.settle(); return; }
        a.entries = [];
        a.loading = false;
        a.error = e.message || T.webTranscriptFailed;
        Waits.tx.settle(renderTranscript);
    });
}

/** Seconds the way the terminal says them: `44s`, `1m 50s`. Not translated — it is a clock. */
function agentClock(seconds) {
    var s = Math.max(0, Math.round(seconds));
    return s < 60 ? s + "s" : Math.floor(s / 60) + "m " + (s % 60) + "s";
}

/** And tokens the way the assistant counts them out loud: `840`, `9.3k`, `84.4k`, `120k`. */
function agentTokens(n) {
    if (n < 1000) return String(n);
    var k = n / 1000;
    return (k < 100 ? k.toFixed(1) : String(Math.round(k))) + "k";
}

/**
 * The agent's header, and the swap that puts it on screen.
 *
 * One header at a time, by hiding the other: two would leave a reader looking at two names with
 * a single transcript underneath and nothing saying which name it belongs to.
 */
function renderAgentHead() {
    var a = S.agent;
    var on = !!a;
    if (els["detail-head"]) els["detail-head"].hidden = on;
    els["agent-head"].hidden = !on;
    els.composer.hidden = on;
    if (!on) return;

    var m = a.meta || {};
    els["agent-back-label"].textContent = T.agentBack;
    els["agent-name"].textContent = m.what || m.type || a.id;
    // Type first, because it says what kind of thing this was; then the state; then what it
    // cost. Those last three are the numbers the terminal prints when an agent lands and
    // nothing in this app was showing anywhere: how long, how many tokens, how many tools.
    var sub = [];
    if (m.type) sub.push(m.type);
    sub.push(m.state === "done" ? T.webAgentDone
           : m.state === "failed" ? T.webAgentFailed : T.agentRunning);
    if (m.seconds) sub.push(agentClock(m.seconds));
    if (m.tokens) sub.push("↓ " + agentTokens(m.tokens));
    if (m.tools) sub.push(fill(T.agentTools, { n: m.tools }));
    els["agent-sub"].textContent = sub.join("  ·  ");
}

function select(id) {
    S.selectedId = id;
    Object.keys(rowNodes).forEach(function (rid) {
        rowNodes[rid].classList.toggle("selected", rid === id);
        rowNodes[rid].setAttribute("aria-selected", rid === id ? "true" : "false");
    });
    var node = rowNodes[id];
    if (node) {
        node.focus({ preventScroll: true });
        node.scrollIntoView({ block: "nearest", behavior: reduced ? "auto" : "smooth" });
    }
}

function move(delta) {
    var list = ordered();
    if (!list.length) return;
    var at = -1;
    for (var i = 0; i < list.length; i++) if (list[i].id === S.selectedId) { at = i; break; }
    var next = at < 0 ? (delta > 0 ? 0 : list.length - 1) : Math.min(list.length - 1, Math.max(0, at + delta));
    select(list[next].id);
}

/* ==========================================================================
   9. Input
   ========================================================================== */

function typing(el) {
    return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
}

document.addEventListener("keydown", function (ev) {
    var key = ev.key;
    var meta = ev.metaKey || ev.ctrlKey;

    // Nothing behind the door is reachable, so nothing behind it takes keys either — and the
    // door is not a dialog you dismiss. There is no page under it to go back to.
    if (!els.door.hidden) return;

    // A confirmation is a decision about one action, not another layer of the page. While it is
    // open, no list or pane shortcut behind it runs; Escape is the one way out from anywhere.
    if (!els["action-confirm"].hidden) {
        if (key === "Escape") {
            ev.preventDefault(); ActionConfirm.close(true);
        }
        return;
    }

    // The three that work from anywhere, including out of a text box, because they are how you
    // get out of the text box or into the session's details.
    if (meta && (key === "k" || key === "K")) {
        ev.preventDefault();
        els.rows.focus();
        if (!S.selectedId) move(1); else select(S.selectedId);
        return;
    }
    if (meta && (key === "j" || key === "J")) {
        ev.preventDefault();
        if (phone()) { els.app.dataset.view = els.app.dataset.view === "detail" ? "list" : "detail"; return; }
        S.paneOpen = !S.paneOpen;
        els.app.dataset.pane = S.paneOpen ? "on" : "off";
        return;
    }
    if (meta && (key === "i" || key === "I")) {
        ev.preventDefault();
        if (!els.info.hidden) { Info.close(); return; }
        // Do not stack one sheet over another. The shortcut remains global while composing, but
        // a visible settings, start or keyboard card owns the next key until it closes.
        if (els.settings.hidden && els.start.hidden && els.keys.hidden) Info.open();
        return;
    }

    if (key === "Escape") {
        if (!els.info.hidden) { Info.close(); return; }
        if (!els.start.hidden) { Start.close(); return; }
        if (!els.settings.hidden) { Settings.close(); return; }
        if (!els.keys.hidden) { els.keys.hidden = true; return; }
        if (document.activeElement === els.filter) {
            if (S.filter) { els.filter.value = ""; S.filter = ""; render(); }
            else { els.filter.blur(); els.rows.focus(); }
            return;
        }
        if (typing(document.activeElement)) { document.activeElement.blur(); return; }
        // An agent is a step inside a session, so Escape gives that step back before it gives
        // the session back. Anything else would close two things for one press.
        if (S.agent) { closeAgent(); return; }
        if (S.openId) { closeDetail(); return; }
        return;
    }

    if (typing(document.activeElement)) return;
    if (meta || ev.altKey) return;
    // A sheet is over the page, so `j` is not "move down the list behind it".
    if (!els.settings.hidden || !els.start.hidden) return;

    switch (key) {
        case "ArrowDown": case "j": ev.preventDefault(); move(1); break;
        case "ArrowUp": case "k": ev.preventDefault(); move(-1); break;
        case "Enter":
            if (S.selectedId) { ev.preventDefault(); openSession(S.selectedId); }
            break;
        case "/":
            ev.preventDefault();
            els.filter.focus();
            els.filter.select();
            break;
        case "g": ev.preventDefault(); els["tx-scroll"].scrollTop = 0; break;
        case "G": ev.preventDefault(); toBottom(); break;
        case "r": ev.preventDefault(); toggleOrder(); break;
        case "?": ev.preventDefault(); els.keys.hidden = !els.keys.hidden; break;
        default: break;
    }
});

/** Pressed in the settings sheet, or `r` from a keyboard. The sheet is redrawn as well as the
 *  transcript: the button that was pressed is a button that says which order it is now. */
function toggleOrder() {
    S.newestFirst = !S.newestFirst;
    Settings.drawOrder();
    renderTranscript();
    els["tx-scroll"].scrollTop = 0;
}

els["list-scroll"].addEventListener("mouseenter", freezeOrder);
els["list-scroll"].addEventListener("mouseleave", thawOrder);
els["list-scroll"].addEventListener("touchstart", freezeOrder, { passive: true });
els["list-scroll"].addEventListener("touchend", function () { setTimeout(thawOrder, 1200); }, { passive: true });

/** A phone-only horizontal gesture layered over rows without borrowing their FLIP transform. */
var SwipeRows = (function () {
    var actionWidth = 126;
    var row = null;
    var openRow = null;
    var startX = 0, startY = 0, startOffset = 0, offset = 0;
    var direction = null;
    var suppressClickUntil = 0;

    function buttonFor(node) { return node && node.querySelector(".swipe-end"); }

    function applyOffset(node, value) {
        node.style.setProperty("--swipe-x", value + "px");
        node.style.setProperty("--swipe-button-x", actionWidth + value + "px");
    }

    function cleanup(node) {
        if (!node) return;
        node.style.removeProperty("--swipe-x");
        node.style.removeProperty("--swipe-button-x");
        delete node.dataset.swipe;
        var button = buttonFor(node);
        if (button) button.hidden = true;
    }

    function settle(node, opened, instant) {
        if (!node) return;
        node.dataset.swipe = "settling";
        applyOffset(node, opened ? -actionWidth : 0);
        openRow = opened ? node : null;
        if (!opened) {
            if (instant || reduced) cleanup(node);
            else setTimeout(function () {
                if (node !== openRow && node.dataset.swipe !== "dragging") cleanup(node);
            }, 200);
        }
    }

    function reset(instant) {
        var active = row;
        var opened = openRow;
        row = null; direction = null;
        if (active && active !== opened) settle(active, false, !!instant);
        if (opened) settle(opened, false, !!instant);
    }

    function touchstart(ev) {
        if (!phone() || !S.write || ev.touches.length !== 1) { reset(true); return; }
        if (ev.target.closest && ev.target.closest(".swipe-end")) return;
        var target = ev.target.closest ? ev.target.closest(".row") : null;
        if (!target || target.classList.contains("starting-row") || target.dataset.closing === "1") {
            reset(false); return;
        }
        if (openRow && openRow !== target) settle(openRow, false, false);
        row = target;
        direction = null;
        startX = ev.touches[0].clientX;
        startY = ev.touches[0].clientY;
        startOffset = openRow === target ? -actionWidth : 0;
        offset = startOffset;
        var button = buttonFor(row);
        if (button) button.hidden = false;
        row.dataset.swipe = "dragging";
        applyOffset(row, offset);
    }

    function touchmove(ev) {
        if (!row || ev.touches.length !== 1) return;
        var dx = ev.touches[0].clientX - startX;
        var dy = ev.touches[0].clientY - startY;
        if (!direction) {
            if (Math.max(Math.abs(dx), Math.abs(dy)) < 8) return;
            direction = Math.abs(dx) > Math.abs(dy) ? "horizontal" : "vertical";
            if (direction === "vertical") { settle(row, startOffset < 0, true); row = null; }
        }
        if (direction !== "horizontal") return;
        ev.preventDefault();
        offset = Math.max(-actionWidth, Math.min(0, startOffset + dx));
        applyOffset(row, offset);
    }

    function touchend() {
        if (!row) return;
        if (direction === "horizontal") {
            suppressClickUntil = Date.now() + 450;
            settle(row, offset < -actionWidth * 0.42, false);
        } else {
            settle(row, startOffset < 0, true);
        }
        row = null; direction = null;
    }

    els["list-scroll"].addEventListener("touchstart", touchstart, { passive: true });
    els["list-scroll"].addEventListener("touchmove", touchmove, { passive: false });
    els["list-scroll"].addEventListener("touchend", touchend, { passive: true });
    els["list-scroll"].addEventListener("touchcancel", function () { reset(true); }, { passive: true });
    els["list-scroll"].addEventListener("scroll", function () { reset(false); }, { passive: true });

    document.addEventListener("touchstart", function (ev) {
        if (openRow && !openRow.contains(ev.target)) reset(false);
    }, { capture: true, passive: true });

    document.addEventListener("click", function (ev) {
        var action = ev.target.closest ? ev.target.closest(".swipe-end") : null;
        if (action) {
            ev.preventDefault(); ev.stopPropagation();
            if (!phone() || !S.write) { reset(true); return; }
            var target = action.closest(".row");
            var id = target && target.dataset.id;
            reset(true);
            if (id) ActionConfirm.open("end", id, target);
            return;
        }
        if (Date.now() < suppressClickUntil) {
            ev.preventDefault(); ev.stopPropagation(); return;
        }
        if (!openRow) return;
        var wasOpen = openRow;
        reset(false);
        if (wasOpen.contains(ev.target)) { ev.preventDefault(); ev.stopPropagation(); }
    }, true);

    window.addEventListener("resize", function () { if (!phone()) reset(true); });
    new MutationObserver(function () {
        if (openRow && (!document.documentElement.contains(openRow)
                        || openRow.getAttribute("aria-hidden") === "true")) reset(true);
    }).observe(els.rows, { childList: true, subtree: true, attributes: true,
                          attributeFilter: ["aria-hidden"] });

    return { reset: reset };
})();

els.filter.addEventListener("input", function () { S.filter = els.filter.value; renderList(); });
/**
 * Refresh, and be seen doing it.
 *
 * A local transcript comes back in a few milliseconds, which is faster than anybody can see: a
 * spinner that appears and vanishes inside three frames is not feedback, it is a flicker. So the
 * working state is held for a beat whatever the network does — long enough to have been a state
 * somebody watched — and then goes. Not disabled while it is out: on a phone a disabled chip in
 * this header is `display: none`, so disabling it would make the button somebody just pressed
 * disappear. The flag is what stops a second press, and the chip stays where it was put.
 */
var refreshing = false;
els["tx-refresh"].addEventListener("click", function () {
    if (!S.openId || refreshing) return;
    refreshing = true;
    var chip = els["tx-refresh"], started = Date.now(), HELD = 550;
    chip.dataset.busy = "on";
    function done() {
        setTimeout(function () {
            chip.dataset.busy = "off";
            refreshing = false;
        }, Math.max(0, HELD - (Date.now() - started)));
    }
    Promise.resolve(loadTranscript(S.openId, true)).then(done, done);
});
els.back.addEventListener("click", function () { closeDetail(); });
// One listener on the box rather than one per row: the strip repaints every time an agent
// reaches for a tool, and rebinding half a dozen buttons a second to do nothing new is work
// nobody would get back.
els.agents.addEventListener("click", function (ev) {
    var row = ev.target.closest ? ev.target.closest("[data-agent]") : null;
    if (!row) return;
    // An empty id is the root row: the way back to the session, which is what closing an agent
    // is. Clicking it while the session is already what the pane is showing does nothing.
    var id = row.getAttribute("data-agent");
    if (id) openAgent(id); else closeAgent();
});
els["agent-back"].addEventListener("click", function () { closeAgent(); });
els.keys.addEventListener("click", function () { els.keys.hidden = true; });
els.keys.querySelector(".sheet").addEventListener("click", function (ev) { ev.stopPropagation(); });
els.conn.addEventListener("click", function () { if (api.refresh) api.refresh(); });

els["tx-focus"].addEventListener("click", function () {
    if (!S.openId) return;
    api.focus(S.openId).then(function () { toast(T.webShowOnMacAsked); })
        .catch(function (e) { toast(e.message, true); });
});

els["detail-focus"].addEventListener("click", function () {
    if (!S.openId) return;
    SessionActions.toggle(els["detail-focus"]);
});

els["detail-actions-title"].addEventListener("click", function () {
    if (!S.openId) return;
    SessionActions.toggle(els["detail-actions-title"]);
});

function actionTriggerKey(ev) {
    if (ev.key !== "ArrowDown") return;
    ev.preventDefault(); ev.stopPropagation();
    SessionActions.open(ev.currentTarget);
    var first = SessionActions.items()[0];
    if (first) first.focus({ preventScroll: true });
}
els["detail-focus"].addEventListener("keydown", actionTriggerKey);
els["detail-actions-title"].addEventListener("keydown", actionTriggerKey);

/**
 * The project mark and its title are two handles for the same menu. Bringing the terminal
 * forward is a menu action of its own, so opening the menu never moves focus away from the
 * browser. Git is a read-only view; `commit` and `push` are ordinary prompts, while ending
 * uses the server's named two-step route so the assistant quits cleanly before its terminal tab
 * is closed.
 */
var SessionActions = {
    opener: null,
    ticket: 0,
    settlingEnd: false,

    onGit: function () {
        return els["session-actions-git"].dataset.place === "current";
    },

    level: function (name, focus) {
        var git = name === "git";
        var main = els["session-actions-main"], child = els["session-actions-git"];
        main.dataset.place = git ? "left" : "current";
        child.dataset.place = git ? "current" : "right";
        main.setAttribute("aria-hidden", git ? "true" : "false");
        child.setAttribute("aria-hidden", git ? "false" : "true");
        main.toggleAttribute("inert", git);
        child.toggleAttribute("inert", !git);
        if (focus) {
            var first = this.items()[0];
            if (first) first.focus({ preventScroll: true });
        }
    },

    items: function () {
        var level = this.onGit() ? els["session-actions-git"] : els["session-actions-main"];
        return Array.prototype.slice.call(level.querySelectorAll("button:not(:disabled)"));
    },

    open: function (opener) {
        if (!S.openId) return;
        this.opener = opener || this.opener || els["detail-focus"];
        this.level("main", false);
        els["session-actions"].hidden = false;
        els["detail-focus"].setAttribute("aria-expanded", "true");
        els["detail-actions-title"].setAttribute("aria-expanded", "true");
    },

    close: function (restore) {
        if (els["session-actions"].hidden) return;
        els["session-actions"].hidden = true;
        this.level("main", false);
        els["detail-focus"].setAttribute("aria-expanded", "false");
        els["detail-actions-title"].setAttribute("aria-expanded", "false");
        if (restore && this.opener && document.contains(this.opener)) {
            this.opener.focus({ preventScroll: true });
        }
        this.opener = null;
    },

    toggle: function (opener) {
        if (els["session-actions"].hidden) this.open(opener); else this.close();
    },

    focusMac: function () {
        var id = S.openId;
        if (!id || !S.write) return;
        this.close();
        api.focus(id).then(function () { toast(T.webShowOnMacAsked); })
            .catch(function (e) { toast(e.message, true); });
    },

    prompt: function (action, sessionID) {
        var id = sessionID || S.openId;
        if (!id || !S.write) return;
        // The reader can switch sessions during the HTTP trip. Remember the target's transcript
        // before that happens, so an older identical command cannot claim this new local turn.
        var known = Optimistic.known(S.tx.id === id ? S.tx.entries : []);
        this.close();
        api.send(id, action, []).then(function () {
            Optimistic.add(id, action, 0, known);
            if (S.openId === id && !S.agent) {
                renderTranscript();
                loadTranscript(id, true);
            }
            toast(action + " ✓");
        })
            .catch(function (e) { toast(e.message, true); });
    },

    end: function (sessionID) {
        var id = sessionID || S.openId;
        // The answer matters to the confirmation sheet, which has already disabled both of its
        // buttons on the assumption that a request is on its way: `false` is the only thing that
        // tells it nothing is coming back, and that it has to let go of itself.
        if (!id || !S.write || closingID) return false;
        var self = this;
        var ticket = ++this.ticket;
        this.close();
        closingID = id;
        this.settlingEnd = false;
        Waits.end.start();
        render();
        ActionConfirm.sync();
        api.end(id).then(function () {
            self.finishEnd(id, ticket, true);
        }).catch(function (e) {
            self.finishEnd(id, ticket, false, e);
        });
        return true;
    },

    /** One ending, whichever answer arrives first. The stream can prove the row is gone before
     *  the POST returns; once either has answered, the other is only the tail of the same trip
     *  and must not clear or toast over whatever the reader did next. */
    finishEnd: function (id, ticket, ok, error) {
        if (closingID !== id || ticket !== this.ticket || this.settlingEnd) return;
        var self = this;
        this.settlingEnd = true;
        Waits.end.settle(function () {
            if (closingID !== id || ticket !== self.ticket) return;
            closingID = null;
            self.settlingEnd = false;
            self.ticket += 1;
            ActionConfirm.finish();
            if (ok && S.openId === id) closeDetail();
            else render();
            toast(ok ? T.webEndSession + " ✓" : ((error && error.message) || T.webRequestFailed), !ok);
        });
    },

    gone: function (id) {
        if (closingID === id) this.finishEnd(id, this.ticket, true);
    }
};

/**
 * A read-only view of the open session's repository, occupying the transcript's space.
 *
 * It owns no cache beyond the time it is visible. Opening and refreshing both ask Git at that
 * moment, and a ticket makes an answer for the previous session harmless if the reader moves on
 * while it is in flight.
 */
var GitPanel = (function () {
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
            forId = S.openId;
            els["git-panel"].hidden = false;
            els["pane-detail"].dataset.git = "on";
            load();
            els["git-close"].focus({ preventScroll: true });
        },

        close: function (restore) {
            if (els["git-panel"].hidden) return;
            ticket += 1;
            forId = null; snapshot = null; loading = false; error = null;
            els["git-panel"].hidden = true;
            delete els["pane-detail"].dataset.git;
            if (restore && !els["detail-focus"].disabled) {
                els["detail-focus"].focus({ preventScroll: true });
            }
        },

        refresh: function () { if (forId) load(); },
        follow: function () { this.close(false); }
    };
})();

/** The second press before a session-changing action reaches the transport. */
var ActionConfirm = {
    pending: null,
    busy: false,

    open: function (kind, sessionID, opener) {
        var id = sessionID || S.openId;
        if (!id || !S.write) return;
        var action = kind === "end" ? T.webEndSession : kind;
        var returnFocus = opener || SessionActions.opener || els["detail-focus"];
        SessionActions.close();
        this.pending = { id: id, kind: kind, action: action, opener: returnFocus };
        this.busy = false;
        els["action-confirm-sheet"].dataset.kind = kind;
        els["action-confirm-title"].textContent = kind === "end"
            ? T.webConfirmEndTitle : fill(T.webConfirmActionTitle, { action: action });
        els["action-confirm-say"].textContent = kind === "end"
            ? T.webConfirmEndSay : fill(T.webConfirmActionSay, { action: action });
        els["action-confirm"].hidden = false;
        this.sync();
        els["action-confirm-go"].focus({ preventScroll: true });
    },

    close: function (restore) {
        if (els["action-confirm"].hidden || this.busy) return;
        var opener = this.pending && this.pending.opener;
        this.pending = null;
        this.sync();
        els["action-confirm"].hidden = true;
        if (restore && opener && document.contains(opener)) {
            opener.focus({ preventScroll: true });
        }
    },

    run: function () {
        var pending = this.pending;
        if (!pending || this.busy) return;
        if (pending.kind === "end") {
            // The decision stays on screen until the Mac has answered. Disabling both ways out
            // makes the one request the only action in flight; the spinner itself waits for the
            // shared 150ms threshold, so a genuinely fast close still looks instant.
            this.busy = true;
            this.sync();
            // A refusal — the write switch went off under this sheet, or a close is already in
            // flight — leaves nothing in flight to release these buttons, and both ways out of
            // the sheet are now disabled. So the sheet undoes itself rather than sitting there
            // spinning at somebody who cannot leave it.
            if (!SessionActions.end(pending.id)) {
                this.busy = false;
                this.sync();
                this.close(false);
            }
            return;
        }
        this.close(false);
        SessionActions.prompt(pending.action, pending.id);
    },

    sync: function () {
        setConfirmSpin(null);
        els["action-confirm-sheet"].setAttribute("aria-busy", this.busy ? "true" : "false");
        els["action-confirm-cancel"].disabled = this.busy;
        els["action-confirm-go"].disabled = this.busy;
        if (this.busy && Waits.end.visible) {
            els["action-confirm-go"].innerHTML = '<span class="busy"><canvas></canvas><span></span></span>';
            els["action-confirm-go"].querySelector(".busy span").textContent = T.webClosing;
            setConfirmSpin(els["action-confirm-go"].querySelector("canvas"));
            drawSpinner(confirmSpin, spinPhase);
        } else {
            els["action-confirm-go"].textContent = T.webConfirm;
        }
    },

    finish: function () {
        this.busy = false;
        this.sync();
        this.close(false);
    }
};

els["session-actions"].addEventListener("click", function (ev) {
    var action = ev.target.closest ? ev.target.closest("[data-action]") : null;
    if (action) { ActionConfirm.open(action.dataset.action); return; }
    if (ev.target.closest && ev.target.closest("#session-focus")) {
        SessionActions.focusMac(); return;
    }
    // A read, not an action: nothing is sent, so there is no confirmation to cross. The menu
    // closes and the card opens over the transcript.
    if (ev.target.closest && ev.target.closest("#session-info")) {
        SessionActions.close(); Info.open(); return;
    }
    if (ev.target.closest && ev.target.closest("#session-git-more")) {
        SessionActions.level("git", true); return;
    }
    if (ev.target.closest && ev.target.closest("#session-actions-back")) {
        SessionActions.level("main", false);
        els["session-git-more"].focus({ preventScroll: true });
        return;
    }
    if (ev.target.closest && ev.target.closest("#session-git")) {
        GitPanel.open(); return;
    }
    if (ev.target.closest && ev.target.closest("#session-end")) ActionConfirm.open("end");
});

els["git-refresh"].addEventListener("click", function () { GitPanel.refresh(); });
els["git-close"].addEventListener("click", function () { GitPanel.close(true); });
els["git-panel"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape") return;
    ev.preventDefault(); ev.stopPropagation(); GitPanel.close(true);
});

els["action-confirm-cancel"].addEventListener("click", function () {
    ActionConfirm.close(true);
});
els["action-confirm-go"].addEventListener("click", function () { ActionConfirm.run(); });
els["action-confirm"].addEventListener("click", function () { ActionConfirm.close(true); });
els["action-confirm-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["action-confirm"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Tab") return;
    if (ActionConfirm.busy) { ev.preventDefault(); return; }
    var items = [els["action-confirm-cancel"], els["action-confirm-go"]];
    var at = items.indexOf(document.activeElement);
    if ((!ev.shiftKey && at === items.length - 1) || (ev.shiftKey && at <= 0)) {
        ev.preventDefault(); items[ev.shiftKey ? items.length - 1 : 0].focus();
    }
});

els["session-actions"].addEventListener("keydown", function (ev) {
    if ((ev.key === "ArrowLeft" || ev.key === "Escape") &&
        SessionActions.onGit()) {
        ev.preventDefault(); ev.stopPropagation();
        SessionActions.level("main", false);
        els["session-git-more"].focus({ preventScroll: true });
        return;
    }
    if (["ArrowDown", "ArrowUp", "Home", "End"].indexOf(ev.key) < 0) return;
    ev.preventDefault(); ev.stopPropagation();
    var items = SessionActions.items();
    if (!items.length) return;
    var at = items.indexOf(document.activeElement), next;
    if (ev.key === "Home") next = 0;
    else if (ev.key === "End") next = items.length - 1;
    else if (ev.key === "ArrowDown") next = (at + 1 + items.length) % items.length;
    else next = (at - 1 + items.length) % items.length;
    items[next].focus({ preventScroll: true });
});

document.addEventListener("pointerdown", function (ev) {
    if (els["session-actions"].hidden ||
        ev.target.closest(".detail-actions, #detail-actions-title")) return;
    SessionActions.close();
});

document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape" || els["session-actions"].hidden) return;
    if (SessionActions.onGit()) return;
    ev.preventDefault(); ev.stopPropagation(); SessionActions.close(true);
}, true);

// Back on a phone: the pushState above put us here, so popping means "list".
window.addEventListener("popstate", function () {
    if (!phone()) return;
    // Innermost first, and only one step per gesture: the entry pushed when an agent was opened
    // is the one being popped, so it gives back the agent and leaves the session where it was.
    if (S.agent) { closeAgent(); return; }
    if (els.app.dataset.view === "detail") closeDetail();
});

els["stale-go"].addEventListener("click", function () { location.reload(); });
els["stale-shut"].addEventListener("click", function () { Build.hush(); });

/* ---- arriving at a session from somewhere else ---------------------------
 *
 * A notification about a session carries `/#session=<id>`, and tapping it landed on the session
 * list. Three things had to be true for it to land on the session; this file was the third.
 *
 * **A fragment is only read when a document loads.** Sending an already-open page to
 * `/#session=abc` changes the address and navigates nothing — same document, no reload, and this
 * page had no other opinion about the fragment, so the tap put the app in front of somebody at
 * the screen they were already on. The service worker now sends a message as well, for the case
 * where it cannot navigate the client at all; both roads end here.
 *
 * The id is held rather than acted on when the list has not arrived yet: a cold start routes
 * before it knows what sessions exist, and `onSessions` tries again with every list.
 */
var wantedSession = null;

function sessionInHash(hash) {
    var found = /(?:^|[#&])session=([^&]*)/.exec(String(hash || ""));
    if (!found || !found[1]) return null;
    try { return decodeURIComponent(found[1]); } catch (e) { return found[1]; }
}

function routeTo(hash) {
    var id = sessionInHash(hash);
    if (!id) return;
    wantedSession = id;
    openWanted();
}

/** Open the session the URL asked for, if it is in the list yet. */
function openWanted() {
    if (!wantedSession) return false;
    if (!byId(wantedSession)) return false;
    var id = wantedSession;
    wantedSession = null;
    // A notification means there is something new to read. Force a transcript read even when
    // this session was already open and its fragment therefore routes to the same screen.
    openSession(id, false, true);
    return true;
}

window.addEventListener("hashchange", function () { routeTo(location.hash); });

// The service worker's `{type: "navigate", url}`. It is sent to a focused client instead of
// navigating it, because a client the worker does not control cannot be navigated at all and a
// URL that differs only in its fragment would not reload one that it could.
if ("serviceWorker" in navigator) {
    navigator.serviceWorker.addEventListener("message", function (ev) {
        var data = ev && ev.data;
        if (!data || data.type !== "navigate" || typeof data.url !== "string") return;
        var cut = data.url.indexOf("#");
        var hash = cut < 0 ? "" : data.url.slice(cut);
        if (!sessionInHash(hash)) return;      // `/` — the test push, and nothing to route to
        // Written into the address as well as acted on, so that a reload from here lands in the
        // same place. Setting it fires `hashchange`, which routes; when it is already what we
        // were sent, nothing fires and this does the routing itself.
        if (hash === location.hash) routeTo(hash);
        else location.hash = hash;
    });
}

/* ---- the settings sheet -------------------------------------------------- */

/**
 * What is true of this browser on this device, which is a different question from anything in
 * the list behind it — so it is somewhere else rather than louder.
 *
 * It came out of a footer that held a "Stop" button and a sentence, permanently, on every screen.
 * Turning notifications off again is a once-a-year press and it was costing a row of a phone
 * display for ever; what stays in the flow is the one state that is asking to be pressed, and
 * everything else moved in here behind the wordmark.
 */
var Settings = (function () {
    var testing = false;

    /** What just happened, said in the sheet rather than in a toast. A toast has gone by the
     *  time somebody has looked at their lock screen to see whether the test arrived. */
    function say(words, calm) {
        els["settings-notify-said"].textContent = words || "";
        els["settings-notify-said"].className = "said" + (calm ? " calm" : "");
    }

    return {
        busy: function () { return testing; },

        open: function () {
            els.settings.hidden = false;
            say("");
            els["settings-version"].textContent =
                S.version ? fill(T.webSettingsVersion, { v: S.version }) : "";
            Push.redraw();
            this.drawAssistantIcons();
            this.drawOrder();
            els["settings-close"].focus({ preventScroll: true });
        },

        close: function () { els.settings.hidden = true; },
        toggle: function () { if (els.settings.hidden) this.open(); else this.close(); },

        drawAssistantIcons: function () {
            var button = els["settings-assistant-icons"];
            button.classList.toggle("on", S.assistantIcons);
            button.setAttribute("aria-pressed", S.assistantIcons ? "true" : "false");
            els["settings-assistant-icons-marks"].innerHTML =
                assistantLogo("claude") + assistantLogo("codex");
        },

        /**
         * The transcript's order, as it was drawn in the header it came from: the words say
         * which order this *is*, the arrow says the same thing in a shape and turns over with
         * it, and the hover text says what pressing it will do.
         */
        drawOrder: function () {
            els["settings-order-label"].textContent =
                S.newestFirst ? T.webOrderNewest : T.webOrderOldest;
            els["settings-order"].classList.toggle("on", S.newestFirst);
        },

        /** The notifications block, drawn from the state Push worked out. */
        drawNotify: function (state, busy) {
            els["settings-notify-say"].textContent = {
                homescreen: T.webNotifyHomeScreen,
                unsupported: T.webNotifyUnsupported,
                blocked: T.webNotifyBlocked,
                on: T.webNotifyOn,
                off: T.webNotifySheetOff
            }[state] || "";

            var go = els["settings-notify-go"];
            go.hidden = !(state === "off" || state === "on");
            go.disabled = busy;
            go.textContent = state === "on" ? (busy ? T.webNotifyStopping : T.webNotifyStop)
                                            : (busy ? T.webNotifyAsking : T.webNotifyGo);
            go.classList.toggle("on", state === "on");

            // Only offered where there is something subscribed for it to reach.
            var test = els["settings-notify-test"];
            test.hidden = state !== "on";
            test.disabled = testing;
            test.textContent = testing ? T.webSending : T.webNotifyTest;
        },

        test: function () {
            if (testing || typeof api.pushTest !== "function") return;
            testing = true;
            say("");
            Push.redraw();
            api.pushTest().then(function () {
                say(T.webNotifyTestSent, true);
            }).catch(function (e) {
                // A 409 is not a failure to apologise for: it means this browser believes
                // notifications are on and the Mac has nothing to send to. That is a state with
                // a one-sentence way out of it, and saying the sentence is more use than an
                // error toast with a number in it.
                say(e && e.code === "not_subscribed" ? T.webNotifyTestNone
                    : (e && e.message) || T.webNotifyTestFailed);
            }).then(function () {
                testing = false;
                Push.redraw();
            });
        }
    };
})();

els.brand.addEventListener("click", function () { Settings.toggle(); });
els.settings.addEventListener("click", function () { Settings.close(); });
els["settings-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["settings-close"].addEventListener("click", function () { Settings.close(); });
els["settings-notify-go"].addEventListener("click", function () { Push.toggle(); });
els["settings-notify-test"].addEventListener("click", function () { Settings.test(); });
els["settings-order"].addEventListener("click", toggleOrder);
els["settings-assistant-icons"].addEventListener("click", function () {
    S.assistantIcons = !S.assistantIcons;
    storeBool("clawdline.assistant-icons", S.assistantIcons);
    Settings.drawAssistantIcons();
    renderTranscript();
});

/* ---- starting a session -------------------------------------------------- */

/**
 * A new Claude Code session, started from the sofa.
 *
 * **The page never names a directory.** It shows a list the Mac built — the places `claude` has
 * actually been run in, that are still there — and sends back the opaque id printed on the row
 * it was given. There is no field on that route a path could be written into, so there is
 * nothing here that could be persuaded to start a session somewhere the Mac did not offer.
 *
 * **It is its own sheet.** The one behind the wordmark is what is true of *this browser on this
 * device*; this is a question about the Mac, and putting them together would have made one sheet
 * that answers two different questions. It is one tap from the list and it costs the list
 * nothing — a square on a row that already exists, rather than a control the session list has to
 * make room for on every screen for ever.
 *
 * **The gap is the hard part.** `POST …/start` answers *before the session exists*: the id it
 * gives back is in the same space as every id in `/v1/sessions`, but that session is not in the
 * list yet and asking for it directly would `404` for a moment. So the id is kept and the list
 * is watched — for the **id**, not for `isClaude`, which stays false until `claude` itself is up
 * — and what is on screen meanwhile is both the persistent line under the header and a project-
 * shaped placeholder at the top of the list. The latter remains deliberately outside the
 * session collection: it has no state, transcript, count or keyboard position, only enough of
 * the chosen place to show where the real row is arriving. After fifteen seconds both stop
 * waiting and the line says the tab is open but nothing has reported in. **It never retries.**
 * The tab exists; a retry is a second tab, and "did that work?" has never meant "do it again".
 */
var Start = (function () {
    var HOLD = 15000;    // how long the band waits before it admits it has stopped waiting
    var MANY = 8;        // places, past which a box to filter them earns its row

    var places = null;   // as the Mac sent them; null until an answer has arrived
    var assistants = [];  // what the Mac will start — [{ id, label }], its list and not this one
    var with_ = null;     // which of them the next press opens; null until the list arrives
    var loading = false;
    var pressing = null; // the place being started, while that one request is in flight
    var find = "";       // what has been typed into the sheet's filter
    var wait = null;     // { id, from, late, place } — started, and not in the list yet
    var timer = null;
    var placeholderNode = null;
    // Held at the placeholder's position through the first following stream update. That first
    // paint is the promised in-place replacement; after it, ordinary list ordering takes over
    // through the same FLIP path as every other move rather than making the arrival itself jump.
    var landed = null;

    function say(words) { els["start-say"].textContent = words || ""; }
    function said(words) { els["start-said"].textContent = words || ""; }

    /**
     * What a refusal means, said in this page's own words.
     *
     * The server's `message` is English, and the reader may not be — so the codes that have a
     * translated sentence get it, and the terminal's name comes out of the error object rather
     * than out of the sentence it was written into. Everything else is one dead end: `forbidden`
     * is a token without the capability, `bad_request` is this page having sent something wrong,
     * and neither is a thing the person holding the phone can do anything about.
     */
    function why(e) {
        var code = e && e.code;
        if (code === "offline") return e.message;          // already this page's own sentence
        if (code === "write_disabled") return T.webStartOff;
        if (code === "not_found") return T.webStartGone;
        // Both 409s carry `app`. Without it there is no sentence to write — a translation with
        // `{app}` still in it is worse than the plain refusal — so that falls through.
        if (e && e.app && code === "terminal_closed") return fill(T.webStartTerminalClosed, { app: e.app });
        if (e && e.app && code === "terminal_unsupported") return fill(T.webStartTerminalUnsupported, { app: e.app });
        return T.webStartFailed;
    }

    function matching() {
        var q = find.trim().toLowerCase();
        return (places || []).filter(function (p) {
            if (!q) return true;
            return ((p.label || "") + " " + (p.path || "")).toLowerCase().indexOf(q) >= 0;
        });
    }

    /**
     * The two chips that say what a press opens.
     *
     * **Only when there is a choice.** One assistant on the Mac and this is a control with
     * nothing to control — so it is not drawn at all, and the sheet is exactly what it was
     * before Codex existed. The list comes from the Mac; this page does not know what is
     * installed and does not guess.
     */
    function drawWith() {
        var row = els["start-with"];
        row.hidden = assistants.length < 2;
        if (row.hidden) { row.innerHTML = ""; return; }
        row.innerHTML = "";
        var label = document.createElement("span");
        label.className = "with-label";
        label.textContent = T.webStartWith;
        row.appendChild(label);
        assistants.forEach(function (a) {
            var chip = document.createElement("button");
            chip.type = "button";
            chip.className = "chip" + (a.id === with_ ? " on" : "");
            chip.textContent = a.label || a.id;
            // Shut while a start is in flight, for the same reason every row is: the press that
            // is already on its way is the one that decides, and changing this under it would
            // only mislead about what opened.
            chip.disabled = !!pressing || !!wait;
            chip.setAttribute("aria-pressed", a.id === with_ ? "true" : "false");
            chip.onclick = function () { with_ = a.id; draw(); };
            row.appendChild(chip);
        });
    }

    function draw() {
        setStartSpin(null);
        if (els.start.hidden) return;
        var list = els["start-list"];
        var box = els["start-filter"];

        // Sending switched off, and that is the whole sheet. The control is still here and it
        // still opens — a button that fails on press teaches nothing, and the sentence names the
        // switch and where it lives. Nothing is fetched: there is nothing to press.
        if (!S.write) {
            say(T.webStartOff);
            box.hidden = true;
            els["start-with"].hidden = true;
            list.innerHTML = "";
            return;
        }

        drawWith();

        say(wait ? T.webStartWaiting
            : (loading && !places) ? T.webLoading
            : (places && !places.length) ? T.webStartEmpty
            : T.webStartPick);

        // Forty is the most the Mac will ever offer and three fit on a phone without scrolling.
        // Under nine, a box to narrow them down is furniture in front of the answer.
        box.hidden = !(places && places.length > MANY);
        if (box.hidden && box.value) { box.value = ""; find = ""; }

        list.innerHTML = "";
        matching().forEach(function (p) {
            var li = document.createElement("li");
            var row = document.createElement("button");
            row.type = "button";
            row.className = "place";
            row.dataset.id = p.id;
            // One press. While a start is in flight, or while one is settling, every row is
            // shut — the second press is a second tab and nobody ever wanted two.
            row.disabled = !!pressing || !!wait;
            if (pressing === p.id) row.dataset.busy = "1";
            row.innerHTML = '<canvas></canvas><span class="name"></span><span class="where"></span>';

            // The same mark the session list draws, drawn by the same code, and still optional.
            var mark = row.querySelector("canvas");
            if (!drawIcon(mark, p.icon, 4)) mark.classList.add("none");

            var name = row.querySelector(".name");
            name.textContent = p.label || p.path;
            name.style.color = p.icon ? tint(p.icon.accent) : "";
            // The path is here so two projects with the same name can be told apart, and it is
            // stood aside for the one line that matters more while a row is being pressed.
            var where = row.querySelector(".where");
            if (pressing === p.id && Waits.startPress.visible) {
                where.innerHTML = '<canvas class="start-spin"></canvas><span></span>';
                where.querySelector("span").textContent = T.webStarting;
                setStartSpin(where.querySelector(".start-spin"));
                drawSpinner(startSpin, spinPhase);
            } else {
                where.textContent = pressing === p.id ? T.webStarting : shortPath(p.path);
            }

            li.appendChild(row);
            list.appendChild(li);
        });
    }

    /** Asked afresh every time the sheet opens: a directory can go away between two looks, and
     *  the list is sorted by when each was last worked in. The old one stays on screen while
     *  the new one is on its way — a list that blanks itself to refetch is a flicker. */
    function load() {
        if (loading || typeof api.places !== "function") return;
        loading = true;
        draw();
        api.places().then(function (d) {
            places = (d && d.places) || [];
            // The Mac's list, not this page's. Whether Codex is installed is a question only
            // that end can answer, and a chip for something that is not there opens a tab
            // saying "command not found".
            assistants = (d && d.assistants) || [];
            if (!with_ || !assistants.some(function (a) { return a.id === with_; })) {
                with_ = assistants.length ? assistants[0].id : null;
            }
        }).catch(function (e) {
            places = places || [];
            said(why(e));
        }).then(function () {
            loading = false;
            draw();
        });
    }

    function press(id) {
        if (pressing || wait || !S.write || typeof api.startPlace !== "function") return;
        var place = null;
        (places || []).some(function (p) {
            if (p.id !== id) return false;
            place = p;
            return true;
        });
        pressing = id;
        said("");
        draw();
        Waits.startPress.start();
        api.startPlace(id, with_).then(function (d) {
            Waits.startPress.settle(function () {
                pressing = null;
                // From here the tab exists on the Mac. Nothing after this line is allowed to read
                // as "it might not have worked", and nothing after it presses this again.
                began(d && d.id, place);
            });
        }).catch(function (e) {
            Waits.startPress.settle(function () {
                pressing = null;
                if (e && e.code === "write_disabled") {
                    // The switch was turned off while this sheet was open. The server is the one
                    // that knows, so take its word for it — the composer answers to the same flag.
                    S.write = false;
                    renderComposer();
                } else if (e && e.code === "not_found") {
                    // That directory has gone since the list was built. It is the list that is
                    // wrong rather than the press, so the list is what gets asked again.
                    places = null;
                    load();
                }
                said(why(e));
                draw();
            });
        });
    }

    /** Started. The sheet has done its job, and what is left is a wait. */
    function began(id, place) {
        close();
        if (!id) {
            // A reply with no id is nothing to watch the list for. The tab was still opened —
            // that is what `ok` meant — so this says now what the fifteen seconds would have.
            band(T.webStartSlow, true);
            return;
        }
        wait = { id: id, from: S.openId, late: false, place: place || {} };
        band(T.webStartWaiting, false);
        renderList();
        clearTimeout(timer);
        timer = setTimeout(function () {
            if (!wait) return;
            wait.late = true;
            band(T.webStartSlow, true);
            renderList();
        }, HOLD);
    }

    function band(words, slow) {
        els.starting.dataset.state = slow ? "slow" : "waiting";
        // Shown before it is written into: it is a live region, and a change made while it is
        // still `hidden` is a change nothing was listening to.
        els.starting.hidden = false;
        els["starting-say"].textContent = words;
        // The spinner rides the list's clock — see `bandSpin`. Nothing turns once the wait has
        // been given up on: there is nothing left to be waiting for.
        setBandSpin(slow ? null : els["starting-spin"]);
        if (bandSpin) drawSpinner(bandSpin, spinPhase);
    }

    function hideBand() {
        els.starting.hidden = true;
        setBandSpin(null);
    }

    function open() {
        els.start.hidden = false;
        said("");
        if (S.write && !wait) load();
        draw();
        els["start-close"].focus({ preventScroll: true });
    }

    function close() {
        if (pressing) return;
        els.start.hidden = true;
        setStartSpin(null);
    }

    function arrange(list) {
        var id = wait ? wait.id : landed;
        if (!id) return list;
        for (var i = 0; i < list.length; i++) {
            if (list[i].id !== id) continue;
            return [list[i]].concat(list.slice(0, i), list.slice(i + 1));
        }
        return list;
    }

    /** The list-shaped promise of the place just pressed. Its node is private to Start rather
     *  than entered into `rowNodes`, which is the mechanical guarantee that arrows, filters and
     *  counts cannot mistake it for a session while still letting it occupy the exact geometry
     *  the arriving row will use. */
    function placeholder() {
        if (!wait || wait.late || byId(wait.id)) {
            if (placeholderNode && placeholderNode.parentNode) {
                placeholderNode.parentNode.removeChild(placeholderNode);
            }
            return null;
        }
        if (!placeholderNode) {
            placeholderNode = document.createElement("li");
            placeholderNode.className = "row starting-row";
            placeholderNode.setAttribute("role", "status");
            placeholderNode.innerHTML = '<canvas class="mark"></canvas>' +
                '<div class="title"><span class="label"></span></div>' +
                '<div class="meta"><span class="path"></span></div>' +
                '<div class="state"><canvas class="spin"></canvas><span class="line"></span></div>';
        }
        var place = wait.place || {};
        placeholderNode.setAttribute("aria-label", T.webStartWaiting);
        var mark = placeholderNode.querySelector(".mark");
        if (!drawIcon(mark, place.icon, 4)) mark.classList.add("none");
        else mark.classList.remove("none");
        var title = placeholderNode.querySelector(".title");
        title.querySelector(".label").textContent = place.label || place.path || T.webStarting;
        title.style.color = place.icon ? tint(place.icon.accent) : "";
        placeholderNode.querySelector(".path").textContent = shortPath(place.path);
        placeholderNode.querySelector(".line").textContent = T.webStartWaiting;
        var spin = placeholderNode.querySelector(".spin");
        drawSpinner(spin, spinPhase);
        spinners.push(spin);
        return placeholderNode;
    }

    return {
        open: open,
        close: close,
        press: press,
        typed: function (value) { find = value; draw(); },
        placeholder: placeholder,
        arrange: arrange,
        arriving: function (id) { return !!(wait && wait.id === id); },

        /** The write switch can flip under an open sheet — `hello` carries it on every
         *  reconnect — and the sheet is a different screen on either side of that. */
        sync: draw,

        /** Let go of the wait: the reader has read the line and closed it. A session that
         *  turns up afterwards is a row in the list like any other. */
        dismiss: function () {
            wait = null;
            clearTimeout(timer);
            timer = null;
            hideBand();
            renderList();
        },

        /**
         * Every list that arrives, until the one with this session in it.
         *
         * The id is what is watched for, not `isClaude` — a tab exists a good second or two
         * before `claude` is a process `ps` can see, and a page waiting for the flag would sit
         * there through a session that had already started fine.
         */
        check: function () {
            if (!wait && landed) {
                landed = null;
                renderList();
                return false;
            }
            if (!wait || !byId(wait.id)) return false;
            var id = wait.id, from = wait.from, late = wait.late;
            landed = late ? null : id;
            wait = null;
            clearTimeout(timer);
            timer = null;
            hideBand();
            draw();
            // Opened, for somebody who has not gone anywhere since pressing it — that is what
            // they asked for, and it is one less tap on a phone. Not if they have opened
            // something else meanwhile, and not after the fifteen seconds have gone by: by then
            // they have been told to look at the Mac, and a transcript arriving over whatever
            // they moved on to is the page having an opinion it has not earned.
            if (late || S.openId !== from) { S.selectedId = id; renderList(); return false; }
            openSession(id);
            return true;
        }
    };
})();

els["start-go"].addEventListener("click", function () { Start.open(); });
els.start.addEventListener("click", function () { Start.close(); });
els["start-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["start-close"].addEventListener("click", function () { Start.close(); });
els["start-filter"].addEventListener("input", function () { Start.typed(this.value); });
els["start-list"].addEventListener("click", function (ev) {
    var row = ev.target.closest ? ev.target.closest(".place") : null;
    if (!row || row.disabled) return;
    Start.press(row.dataset.id);
});
els["starting-close"].addEventListener("click", function () { Start.dismiss(); });

/**
 * One small cache in front of the expensive session-info read. The status line needs those facts
 * while the card is closed, and opening the card immediately afterwards must not make the Mac
 * read the same transcript and run the same `git status` twice. A minute is fresh enough for a
 * glance; ending a turn and the card's Refresh button both ask explicitly for a newer answer.
 */
var SessionFacts = (function () {
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
var StatusLine = (function () {
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
var Info = (function () {
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
    var busy = false;    // a `/model` on its way to the Mac
    var stateSeen = "";  // the session's state at the last draw, so a change redraws the buttons
    var confirming = 0;  // the timer reading back, waiting for a sent `/model` to turn up

    function say(w) { els["info-say"].textContent = w || ""; els["info-say"].hidden = !w; }
    function said(w, calm) {
        els["info-said"].textContent = w || "";
        els["info-said"].className = "said" + (calm ? " calm" : "");
    }
    function why(e) {
        if (e && e.code === "offline") return e.message;   // already this page's own sentence
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
    /** The row the session is on. By prefix, so `claude-haiku-4-5-20251001` finds `claude-haiku-4-5`. */
    function onModel(current, m) { return !!current && (current === m.id || current.indexOf(m.id) === 0); }

    function hero(s, u) {
        var model = s.model || (u && u.model) || "";
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
            '<div class="who">' + assistantLogo(s.assistant) + "<span>" + esc(s.assistant || T.webInfoUnknown) + "</span></div>" +
            '<div class="model">' + esc(model || T.webInfoUnknown) + "</div>" +
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
            links = d.links || d.deploy || [], models = d.models || [];
        // The model is whatever the transcript last named — a reply, or a `/model` nobody has
        // replied to yet, whichever of the two is newer. So a session that switched mid-way shows
        // what it is on now rather than what it began on, a session that has only ever been
        // switched shows that rather than nothing, and a `/model` this card sent stops being
        // pending the moment the record agrees with it.
        var current = s.model || (u && u.model) || "";
        if (pending && models.some(function (m) { return m.command === pending && onModel(current, m); })) pending = null;
        var out = hero(s, u), i = 0;
        // Read-only pairings get no buttons rather than dead ones: there is nothing they could do.
        if (models.length && S.write) out += sec(++i, T.webInfoSwitchModel, "", modelsHTML(models, current));
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

    return {
        open: function () {
            if (!S.openId) return;
            forId = S.openId;
            data = SessionFacts.peek(forId);
            drawn = false;
            pending = null;
            busy = false;
            clearTimeout(confirming);
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
            busy = false;
            clearTimeout(confirming);
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

/* ---- notifications ------------------------------------------------------- */

/**
 * Web Push: the phone buzzes when a session is waiting for an answer.
 *
 * Four things can be true here and only one of them is "on", so the footer says which. A button
 * that has been pressed and did nothing is the worst of the four — that is what a permission the
 * reader denied looks like from inside the page, and the only cure for it is in the browser's own
 * settings, which is a sentence rather than a control.
 *
 * **On iOS this only works from the home screen.** Not "works badly" — the API is absent in a
 * Safari tab, so there is nothing to press and nothing to explain afterwards. The one sentence
 * that gets somebody from there to a working notification is therefore the whole feature until
 * they have read it, and it is shown instead of a button rather than beside one.
 *
 * The service worker is the app's own `/sw.js`. It already knows how to draw a notification and
 * what to do when one is tapped; this end registers it and hands it a subscription.
 */
var Push = (function () {
    var registration = null;
    var subscribed = false;
    var state = "unsupported";   // unsupported | homescreen | blocked | off | on
    var busy = false;
    /** Whether the subscription has actually been looked up. Until it has, `decide()` answers
     *  "off" because `subscribed` starts false — which is a default and not a reading, and it is
     *  the one state that puts a button on the screen. See the markup.
     *
     *  Only the footer waits on this. The settings sheet is drawn from `state` either way, so a
     *  browser whose worker registers and then never activates — the one case where nothing here
     *  ever settles — still has somewhere to turn notifications on from, and the row along the
     *  bottom of the list is not offering a button that could not have worked. */
    var settled = false;

    /** The id the server gave this subscription, kept so it can be taken back after a reload. */
    function remember(value) {
        try {
            if (value == null) localStorage.removeItem("clawdline.push");
            else localStorage.setItem("clawdline.push", String(value));
        } catch (e) { /* a private window has no storage, and this is not worth failing over */ }
    }
    function recall() {
        try { return localStorage.getItem("clawdline.push"); } catch (e) { return null; }
    }

    function standalone() {
        return window.navigator.standalone === true
            || (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches);
    }

    /** iPadOS calls itself a Mac, and a touch screen is the only tell left. */
    function iOS() {
        var platform = navigator.platform || "";
        return /iP(hone|ad|od)/.test(platform)
            || (/Mac/.test(platform) && navigator.maxTouchPoints > 1);
    }

    function decide() {
        if (iOS() && !standalone()) return "homescreen";
        if (typeof api.pushKey !== "function") return "unsupported";      // mock mode has no server
        if (!window.isSecureContext) return "unsupported";
        if (!("serviceWorker" in navigator) || !("PushManager" in window)) return "unsupported";
        if (typeof Notification === "undefined") return "unsupported";
        if (Notification.permission === "denied") return "blocked";
        return subscribed ? "on" : "off";
    }

    /// The VAPID key arrives as base64url and `subscribe` wants bytes.
    function keyBytes(key) {
        var padded = String(key).replace(/-/g, "+").replace(/_/g, "/");
        while (padded.length % 4) padded += "=";
        var raw = atob(padded);
        var out = new Uint8Array(raw.length);
        for (var i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
        return out;
    }

    /// Safari answered this with a callback for years before it answered with a promise.
    function askPermission() {
        return new Promise(function (done, fail) {
            try {
                var maybe = Notification.requestPermission(function (answer) { done(answer); });
                if (maybe && typeof maybe.then === "function") maybe.then(done, fail);
            } catch (e) { fail(e); }
        });
    }

    function draw() {
        state = busy ? state : decide();
        // **Only the two states somebody can act on from here keep a place in the flow.** Off is
        // an offer and needs a button; on iOS in a tab there is no button to have, and the one
        // sentence that gets somebody to a working notification is the whole feature until they
        // have read it. Everything else — already on, blocked, this browser cannot — is a fact
        // rather than a thing to do, and a fact does not get a permanent row of the screen.
        // `settled` first: a footer that has not been decided yet is not in the flow, whatever
        // the placeholder state says. Appearing a few frames late is a layout shift; appearing
        // and then vanishing is a fault.
        var inFlow = settled && (state === "off" || state === "homescreen");
        els.notify.hidden = !inFlow;
        els.notify.dataset.state = state;

        els["notify-go"].hidden = state !== "off";
        els["notify-go"].disabled = busy;
        els["notify-go-label"].textContent = busy ? T.webNotifyAsking : T.webNotifyGo;
        els["notify-say"].textContent =
            state === "homescreen" ? T.webNotifyHomeScreen : T.webNotifyOff;

        Settings.drawNotify(state, busy);
    }

    function enable() {
        busy = true; draw();
        askPermission().then(function (answer) {
            if (answer !== "granted") { busy = false; draw(); return null; }
            return navigator.serviceWorker.ready.then(function (r) {
                registration = r;
                return api.pushKey();
            }).then(function (d) {
                return registration.pushManager.subscribe({
                    userVisibleOnly: true,
                    applicationServerKey: keyBytes(d.key)
                });
            }).then(function (subscription) {
                return api.pushSubscribe(subscription.toJSON());
            }).then(function (d) {
                subscribed = true;
                remember(d && d.id);
                busy = false; draw();
                // Straight to the sheet, because the moment permission has been granted is the
                // moment somebody wants proof — and the test button is in there. A toast saying
                // "this will work now" is the page asking to be taken on trust.
                Settings.open();
            });
        }).catch(function (e) {
            busy = false; draw();
            toast(e && e.message ? e.message : T.webNotifyOnFailed, true);
        });
    }

    function disable() {
        busy = true; draw();
        var id = recall();
        navigator.serviceWorker.ready.then(function (r) {
            return r.pushManager.getSubscription();
        }).then(function (subscription) {
            return subscription ? subscription.unsubscribe() : null;
        }).then(function () {
            // Told, but not waited on: the subscription is already gone from this browser, and a
            // server that never hears about it will drop it the first time it pushes to nothing.
            return id ? api.pushUnsubscribe(id).catch(function () { return null; }) : null;
        }).then(function () {
            subscribed = false;
            remember(null);
            busy = false; draw();
        }).catch(function (e) {
            busy = false; draw();
            toast(e && e.message ? e.message : T.webNotifyOffFailed, true);
        });
    }

    return {
        redraw: draw,
        start: function () {
            draw();
            // These two are read off this browser rather than off a subscription, so they are
            // known now and there is nothing to wait for.
            if (decide() === "unsupported" || decide() === "homescreen") {
                settled = true;
                draw();
                return;
            }
            navigator.serviceWorker.register("/sw.js").then(function (r) {
                registration = r;
                return navigator.serviceWorker.ready;
            }).then(function (r) {
                return r.pushManager.getSubscription();
            }).then(function (subscription) {
                // Both halves have to agree. A subscription this browser still holds but the app
                // has forgotten — reinstalled, database cleared — would draw as "on" and never
                // arrive, so what is remembered here is the id the server gave back.
                subscribed = !!subscription && !!recall();
                settled = true;
                draw();
            }).catch(function () {
                // No worker means no notifications, and the footer already has a sentence for it.
                settled = true;
                draw();
            });
        },
        toggle: function () {
            if (busy) return;
            if (state === "off") enable();
            else if (state === "on") disable();
        }
    };
})();

els["notify-go"].addEventListener("click", function () { Push.toggle(); });

/* ---- pictures ------------------------------------------------------------ */

/**
 * Photograph a whiteboard, or the error on the other screen, and send it into the session.
 *
 * Everything goes up as a `data:` URL beside the text, and the server decodes it, re-encodes to
 * PNG and hands Claude Code a real image — it arrives as `[Image #1]`, not as a path somebody
 * has to go and read.
 *
 * **The picture is shrunk here rather than sent as it was taken.** A modern phone photograph is
 * three to six megabytes, base64 adds a third to that, and the server's body limit is twenty:
 * two photographs from a 48-megapixel camera would simply fail, and slowly, over the mobile
 * connection somebody is standing on. A long edge of 1600px is more than Claude Code can use —
 * text on a whiteboard is legible well below that — and it turns a forty-second upload into an
 * instant one. Screenshots are kept as PNG unless that comes out too big to send, because a
 * screenshot is text, and JPEG is exactly the wrong thing to do to text.
 */
var Shots = (function () {
    var LONG_EDGE = 1600;
    var QUALITY = 0.82;
    var MAX_COUNT = 6;
    // Measured on the wire, which is what the limit is about: these are the lengths of the
    // `data:` strings, base64 and all. The server refuses a body over 20MB, so this leaves room
    // for the message, the JSON around it and the headers.
    var MAX_EACH = 5 << 20;
    var MAX_TOTAL = 15 << 20;

    var list = [];
    var seq = 0;
    var busy = 0;

    function total() {
        return list.reduce(function (sum, shot) { return sum + shot.url.length; }, 0);
    }

    /// A bitmap from a file, however this browser is willing to give one.
    function decode(file) {
        if (window.createImageBitmap) {
            // `from-image` so a photograph taken sideways is not drawn sideways: the orientation
            // lives in EXIF, and a canvas does not read EXIF.
            return createImageBitmap(file, { imageOrientation: "from-image" })
                .catch(function () { return createImageBitmap(file); });
        }
        return new Promise(function (done, fail) {
            var url = URL.createObjectURL(file);
            var img = new Image();
            img.onload = function () { URL.revokeObjectURL(url); done(img); };
            img.onerror = function () { URL.revokeObjectURL(url); fail(new Error("not a picture")); };
            img.src = url;
        });
    }

    function asDataURL(file) {
        return new Promise(function (done, fail) {
            var reader = new FileReader();
            reader.onload = function () { done(String(reader.result)); };
            reader.onerror = function () { fail(new Error("could not be read")); };
            reader.readAsDataURL(file);
        });
    }

    function shrink(file) {
        return decode(file).then(function (bitmap) {
            var scale = Math.min(1, LONG_EDGE / Math.max(bitmap.width, bitmap.height));
            var canvas = document.createElement("canvas");
            canvas.width = Math.max(1, Math.round(bitmap.width * scale));
            canvas.height = Math.max(1, Math.round(bitmap.height * scale));
            canvas.getContext("2d").drawImage(bitmap, 0, 0, canvas.width, canvas.height);
            if (bitmap.close) bitmap.close();
            var url = file.type === "image/png" ? canvas.toDataURL("image/png") : "";
            if (!url || url.length > MAX_EACH) url = canvas.toDataURL("image/jpeg", QUALITY);
            return { url: url, w: canvas.width, h: canvas.height };
        }).catch(function () {
            // The browser cannot decode it — a HEIC from an iPhone is this, in everything but
            // Safari. The bytes still go: the server re-encodes whatever it is given, and that
            // re-encoding is also how it checks the file really is an image.
            return asDataURL(file).then(function (url) { return { url: url, w: 0, h: 0 }; });
        });
    }

    function draw() {
        els.shots.innerHTML = list.map(function (shot) {
            return '<div class="shot"><img src="' + shot.url + '" alt="' + esc(shot.name) + '">' +
                '<button type="button" class="drop" data-shot="' + shot.id +
                '" aria-label="' + esc(fill(T.webRemoveShot, { name: shot.name })) + '">×</button></div>';
        }).join("");
        renderComposer();
    }

    els.shots.addEventListener("click", function (ev) {
        var handle = ev.target.closest ? ev.target.closest("[data-shot]") : null;
        if (!handle) return;
        var id = handle.getAttribute("data-shot");
        list = list.filter(function (shot) { return String(shot.id) !== id; });
        draw();
    });

    return {
        count: function () { return list.length; },
        busy: function () { return busy > 0; },
        urls: function () { return list.map(function (shot) { return shot.url; }); },
        clear: function () { list = []; draw(); },

        add: function (files) {
            var wanted = Array.prototype.filter.call(files || [], function (f) {
                // A HEIC sometimes arrives with an empty type, so the extension gets a say.
                return /^image\//.test(f.type) || /\.(hei[cf]|jpe?g|png|gif|webp)$/i.test(f.name || "");
            });
            if (!wanted.length) { toast(T.webShotsOnlyPictures, true); return; }
            if (list.length + wanted.length > MAX_COUNT) {
                toast(fill(T.webShotsTooMany, { n: MAX_COUNT }), true);
                wanted = wanted.slice(0, Math.max(0, MAX_COUNT - list.length));
                if (!wanted.length) return;
            }
            busy += wanted.length;
            renderComposer();
            wanted.forEach(function (file) {
                shrink(file).then(function (shot) {
                    if (shot.url.length > MAX_EACH) {
                        toast(T.webShotTooBig, true);
                        return;
                    }
                    if (total() + shot.url.length > MAX_TOTAL) {
                        toast(T.webShotsTooBig, true);
                        return;
                    }
                    list.push({ id: ++seq, url: shot.url, name: file.name || "picture" });
                }).catch(function () {
                    toast(T.webShotUnreadable, true);
                }).then(function () {
                    busy -= 1;
                    draw();
                });
            });
        }
    };
})();

els.attach.addEventListener("click", function () { els.pick.click(); });
els.pick.addEventListener("change", function () {
    Shots.add(els.pick.files);
    // Cleared so that picking the same file twice in a row still counts as a change.
    els.pick.value = "";
});

// Paste, because copying a screenshot and pressing paste is how this is done everywhere else.
// Not while the filter box has the focus: that one takes text and nothing else.
document.addEventListener("paste", function (ev) {
    if (!S.openId || !S.write || closingID === S.openId) return;
    if (document.activeElement === els.filter) return;
    var files = ev.clipboardData && ev.clipboardData.files;
    if (!files || !files.length) return;
    ev.preventDefault();
    Shots.add(files);
});

// Drag and drop. The whole detail pane is the target — somebody dragging a photograph at this
// page is aiming at the conversation, not at a 30-pixel button — and the document swallows the
// drops that miss, because the browser's own answer to those is to navigate away from the page.
(function dropPictures() {
    var pane = els["pane-detail"];
    var depth = 0;
    function carriesFiles(ev) {
        var dt = ev.dataTransfer;
        if (!dt) return false;
        if (dt.types && Array.prototype.indexOf.call(dt.types, "Files") >= 0) return true;
        return !!(dt.files && dt.files.length);
    }
    document.addEventListener("dragover", function (ev) { if (carriesFiles(ev)) ev.preventDefault(); });
    document.addEventListener("drop", function (ev) { if (carriesFiles(ev)) ev.preventDefault(); });
    pane.addEventListener("dragenter", function (ev) {
        if (!carriesFiles(ev) || !S.openId || !S.write || closingID === S.openId) return;
        depth += 1;
        pane.classList.add("dropping");
    });
    pane.addEventListener("dragleave", function () {
        depth = Math.max(0, depth - 1);
        if (!depth) pane.classList.remove("dropping");
    });
    pane.addEventListener("drop", function (ev) {
        depth = 0;
        pane.classList.remove("dropping");
        if (!carriesFiles(ev)) return;
        ev.preventDefault();
        if (!S.openId || !S.write || closingID === S.openId) {
            toast(T.webShotNeedsSession, true); return;
        }
        Shots.add(ev.dataTransfer.files);
    });
})();

/* ---- the composer -------------------------------------------------------- */

/**
 * What is in the box, as text.
 *
 * `innerText` and never `textContent`: the second one strips the line breaks out, and being able
 * to write a second line is the entire reason this is a `contenteditable` rather than a field.
 * A non-breaking space becomes an ordinary one on the way out — a browser puts them in on its
 * own, and one hiding in a shell command somebody pasted is a command that will not run.
 */
function rawMsgText() {
    return String(els.msg.innerText || "").replace(/\u00A0/g, " ");
}
function msgText() { return rawMsgText().trim(); }

/** Whether the placeholder should be showing. See the note on `.composer .msg.blank`. */
function blankness() {
    els.msg.classList.toggle("blank", !msgText());
}

/** The caret at the end of whatever is in the box, for the times the page moves the focus
 *  itself — the start is where it would otherwise land, and nobody wants to type there. */
function caretToEnd() {
    var range = document.createRange();
    range.selectNodeContents(els.msg);
    range.collapse(false);
    var selection = window.getSelection();
    if (!selection) return;
    selection.removeAllRanges();
    selection.addRange(range);
}

/** Text into the box at the caret, keeping the undo stack. `execCommand` is deprecated and is
 *  still the only thing that does that; the range is the fallback for the day it goes. */
function insertText(text) {
    if (!text) return;
    try {
        if (document.execCommand && document.execCommand("insertText", false, text)) return;
    } catch (e) { /* the range, then */ }
    var selection = window.getSelection();
    if (!selection || !selection.rangeCount) return;
    var range = selection.getRangeAt(0);
    range.deleteContents();
    var node = document.createTextNode(text);
    range.insertNode(node);
    range.setStartAfter(node);
    range.collapse(true);
    selection.removeAllRanges();
    selection.addRange(range);
}

/**
 * Skills for the open session, fetched only when `/` (or Codex's native `$`) makes them visible.
 *
 * Nothing from SKILL.md is interpreted here. The Mac hands over a name and one safe line of
 * description; choosing one only writes the assistant's real invocation into the box. The
 * assistant remains the one that resolves, authorises and runs it when the ordinary send happens.
 */
var SkillPicker = (function () {
    var cache = {};                    // session id → catalog
    var loading = {};                  // session id → true
    var matches = [];
    var selected = 0;
    var shown = false;

    function prefix() {
        var session = S.openId ? byId(S.openId) : null;
        return session && session.assistant === "codex" ? "$" : "/";
    }

    function query() {
        var text = rawMsgText();
        var session = S.openId ? byId(S.openId) : null;
        var pattern = session && session.assistant === "codex"
            ? /^[\/$][^\s\/$]*$/ : /^\/[^\s/]*$/;
        return pattern.test(text) ? text.slice(1).toLowerCase() : null;
    }

    function rank(skill, q) {
        var name = String(skill.name || "").toLowerCase();
        if (!q) return 0;
        if (name.indexOf(q) === 0) return 0;
        if (name.split(/[-_:]/).some(function (part) { return part.indexOf(q) === 0; })) return 1;
        if (name.replace(/[-_:]/g, "").indexOf(q.replace(/[-_:]/g, "")) === 0) return 2;
        if (String(skill.description || "").toLowerCase().indexOf(q) >= 0) return 3;
        return null;
    }

    function filtered(items, q) {
        return (items || []).map(function (skill) { return { skill: skill, rank: rank(skill, q) }; })
            .filter(function (row) { return row.rank !== null; })
            .sort(function (a, b) {
                return a.rank - b.rank || String(a.skill.name).localeCompare(String(b.skill.name));
            }).slice(0, 9).map(function (row) { return row.skill; });
    }

    function hide() {
        shown = false; matches = []; selected = 0;
        els["skill-menu"].hidden = true;
        els["skill-menu"].textContent = "";
    }

    function draw(q) {
        matches = filtered(cache[S.openId] || [], q);
        selected = Math.min(selected, Math.max(0, matches.length - 1));
        var menu = els["skill-menu"];
        menu.textContent = "";
        if (!matches.length) { hide(); return; }
        shown = true; menu.hidden = false;
        matches.forEach(function (skill, i) {
            var row = document.createElement("button");
            row.type = "button"; row.className = "skill-option"; row.setAttribute("role", "option");
            row.setAttribute("aria-selected", i === selected ? "true" : "false");
            var command = document.createElement("span");
            command.className = "command"; command.textContent = prefix() + skill.name;
            var description = document.createElement("span");
            description.className = "description"; description.textContent = skill.description || "";
            row.appendChild(command); row.appendChild(description);
            // Keep the soft keyboard open. The click still arrives; only the focus transfer goes.
            row.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
            row.addEventListener("click", function () { selected = i; accept(); });
            menu.appendChild(row);
        });
    }

    function changed() {
        var q = query(), id = S.openId;
        if (q === null || !id) { hide(); return; }
        if (cache[id]) { draw(q); return; }
        hide();
        if (loading[id]) return;
        loading[id] = true;
        api.skills(id).then(function (answer) {
            cache[id] = answer.skills || [];
        }).catch(function () {
            // Autocomplete is a convenience, never a reason the composer should fail. Unknown
            // commands may still be sent and the assistant will give the authoritative answer.
            cache[id] = [];
        }).then(function () {
            delete loading[id];
            if (S.openId === id) changed();
        });
    }

    function move(delta) {
        if (!shown || !matches.length) return false;
        selected = Math.max(0, Math.min(matches.length - 1, selected + delta));
        Array.prototype.forEach.call(els["skill-menu"].children, function (row, i) {
            row.setAttribute("aria-selected", i === selected ? "true" : "false");
            if (i === selected) row.scrollIntoView({ block: "nearest" });
        });
        return true;
    }

    function accept() {
        if (!shown || !matches[selected]) return false;
        els.msg.textContent = prefix() + matches[selected].name + " ";
        blankness(); hide(); caretToEnd(); renderComposer();
        return true;
    }

    return { changed: changed, move: move, accept: accept, close: function () {
        if (!shown) return false; hide(); return true;
    }, visible: function () { return shown; } };
})();

els.msg.addEventListener("input", function () {
    blankness();
    renderComposer();
    SkillPicker.changed();
});

// Nothing goes in while a send is in flight. This rather than switching the editability off,
// which would take the focus and the keyboard with it.
els.msg.addEventListener("beforeinput", function (ev) { if (sending) ev.preventDefault(); });

// Plain text, explicitly, even though `plaintext-only` covers most of it: what a browser does
// with a rich paste is worth being sure about rather than nearly sure about. A pasted picture is
// not text and is left to the document's own handler, which puts it in the attachments.
els.msg.addEventListener("paste", function (ev) {
    var files = ev.clipboardData && ev.clipboardData.files;
    if (files && files.length) return;
    ev.preventDefault();
    insertText((ev.clipboardData || window.clipboardData).getData("text/plain") || "");
    blankness();
    renderComposer();
    SkillPicker.changed();
});
els.msg.addEventListener("keydown", function (ev) {
    if (ev.isComposing || ev.keyCode === 229) return;
    if (ev.key === "ArrowDown" && SkillPicker.move(1)) { ev.preventDefault(); return; }
    if (ev.key === "ArrowUp" && SkillPicker.move(-1)) { ev.preventDefault(); return; }
    if (ev.key === "Tab" && SkillPicker.accept()) { ev.preventDefault(); return; }
    if (ev.key === "Escape" && SkillPicker.close()) { ev.preventDefault(); return; }
    if (ev.key !== "Enter" || ev.shiftKey) return;
    // **Not while an input method is mid-word.** Typing Chinese, Japanese or Korean, Return is
    // how you accept the candidate the IME is offering — it is a keystroke aimed at the input
    // method, not at us, and acting on it sends whatever is in the box before the person has
    // finished the word they are writing. `isComposing` is the browser telling us which one this
    // is; keyCode 229 is the same fact from browsers that predate it. This one is checked on
    // both paths, because on a touch screen the candidate picker is the whole of how Chinese is
    // typed and a Return swallowed there is a word lost.
    if (SkillPicker.accept()) { ev.preventDefault(); return; }
    // **On a touch screen Return is a new line and the button is how you send.** The other way
    // round there is no way to write a second line at all — the on-screen keyboard has one
    // Return and it was spending it on sending — which made this box worse than every messaging
    // app on the same phone. Where there is a real keyboard, Return still sends and Shift-Return
    // still breaks the line, because that is the muscle memory there and taking it away would be
    // its own complaint.
    if (!hasKeyboard()) return;
    ev.preventDefault();
    submit();
});
// Pressing a button takes the focus off whatever had it, and what had it is the box somebody is
// typing in — so on a phone every message would close the keyboard and open it again. The click
// still happens; only the focus change is refused.
els.send.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
els.attach.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
els.composer.addEventListener("submit", function (ev) { ev.preventDefault(); submit(); });

/// True from the moment a send starts until its answer comes back.
///
/// The button is disabled while a send is in flight, and that was the whole guard — which does
/// nothing at all for Return, because Return does not go through the button. Sending takes a
/// couple of hundred milliseconds (it is an osascript round trip at the far end), the box is not
/// cleared until the answer arrives, and a second Return inside that window found the same text
/// still sitting there and sent it again. Two identical messages in somebody's session, and
/// nothing on this page had said no.
///
/// The `Idempotency-Key` does not help here and is not supposed to: it exists so a **retried**
/// request is not a second prompt, and these were two requests the page chose to make.
var sending = false;

function submit() {
    if (sending || Shots.busy()) return;
    if (SkillPicker.accept()) return;
    var text = msgText();
    var pictures = Shots.urls();
    if ((!text && !pictures.length) || !S.openId || S.agent || !S.write || closingID === S.openId) return;
    var sentID = S.openId;
    var stick = atBottom();
    var session = byId(S.openId);
    // A quit line is not a message. Sending it through the ordinary route does make the
    // assistant leave, but the shell (and therefore the iTerm2 tab) immediately drops out of
    // Clawdline's session list, leaving no way for this page to close it afterwards. Join those
    // two steps while the terminal session id is still known. Exact and assistant-specific so a
    // sentence mentioning `/exit`, or Claude being sent Codex's `/quit`, stays an ordinary prompt.
    var quit = session && !pictures.length && text.trim() ===
        (session.assistant === "codex" ? "/quit" : "/exit");
    sending = true;
    renderComposer();
    var request = quit ? api.end(sentID) : api.send(sentID, text, pictures);
    request.then(function () {
        // Every child node goes, not just the words: the placeholder is drawn from what
        // `innerText` says, and a `<br>` the browser left behind would keep the box looking
        // like it still had something in it.
        els.msg.textContent = "";
        blankness();
        if (document.activeElement === els.msg) caretToEnd();
        // Cleared here and not before: a send that failed still has its pictures attached, and
        // the reader can press the button again rather than going back to the camera roll.
        Shots.clear();
        if (quit) closeDetail();
        else if (S.openId === sentID) {
            if (!S.agent) {
                Optimistic.add(sentID, text, pictures.length);
                renderTranscript();
                if (stick) toBottom();
            }
            loadTranscript(sentID, true);
        }
    }).catch(function (e) {
        if (e.code === "write_disabled") {
            // The flag and the truth have drifted apart — believe the answer, not the flag.
            S.write = false;
            renderComposer();
            renderDetailHead();
        }
        // The words are still in the box — they were never taken out of it — so the press can
        // simply be repeated. A composer that clears itself and then fails is a composer that
        // ate somebody's message.
        toast(e.message || T.sendFailed, true);
    }).then(function () { sending = false; renderComposer(); });
}

/* ---- the strip above the keyboard ---------------------------------------- */

/**
 * The `^ v` half of the bar iOS draws over the keyboard.
 *
 * It is there because the document has other things to tab to — the filter box first of all, and
 * the buttons in the header — and it is a pair of arrows that jump the writer out of the message
 * they are writing and into a search field. While the composer has the focus there is nothing
 * else on this page anybody wants to reach with them, so nothing else is in the tab order.
 *
 * **The `✓` is not ours and does not go.** There is no API for it: Safari draws it over every
 * real text field, and the only technique that removes the bar entirely is to stop using one —
 * a `contenteditable` div — which would put input-method composition, paste and the placeholder
 * at risk to tidy up a button. `autocomplete="off"` does not do it either; that attribute is
 * about autofill and has never had anything to say about this strip.
 */
(function keyboardBar() {
    var moved = null;

    function offstage() {
        onstage();
        moved = [];
        var all = document.querySelectorAll("a[href], button, input, select, textarea, [tabindex]");
        for (var i = 0; i < all.length; i++) {
            var el = all[i];
            if (el === els.msg || el.getAttribute("tabindex") === "-1") continue;
            moved.push([el, el.getAttribute("tabindex")]);
            el.setAttribute("tabindex", "-1");
        }
    }

    function onstage() {
        if (!moved) return;
        moved.forEach(function (pair) {
            if (pair[1] === null) pair[0].removeAttribute("tabindex");
            else pair[0].setAttribute("tabindex", pair[1]);
        });
        moved = null;
    }

    els.msg.addEventListener("focus", offstage);
    els.msg.addEventListener("blur", onstage);
    // The transcript is rebuilt under the writer while they write — a session that is working
    // sends a new one every few seconds — and every rebuild puts fresh buttons into the tab
    // order behind their back. So it is done again after a render, while the box still has the
    // focus. Cheap: it is a query and a few attribute writes.
    document.addEventListener("clawdline:rendered", function () {
        if (document.activeElement === els.msg) offstage();
    });
})();

/* ---- pull to refresh, phones only ---------------------------------------- */

(function pullToRefresh() {
    var scroller = els["list-scroll"], pad = els.ptr, label = els["ptr-label"];
    var startY = 0, pulling = false, distance = 0, busy = false;
    var THRESHOLD = 62;

    scroller.addEventListener("touchstart", function (ev) {
        if (busy || scroller.scrollTop > 0 || ev.touches.length !== 1) return;
        startY = ev.touches[0].clientY;
        pulling = true;
        distance = 0;
        pad.classList.add("dragging");
    }, { passive: true });

    scroller.addEventListener("touchmove", function (ev) {
        if (!pulling) return;
        // Resistance, so the list does not feel like it is on a spring — the further it comes,
        // the harder it pulls back, which is the only honest way to say "that is far enough".
        var raw = ev.touches[0].clientY - startY;
        if (raw <= 0) { distance = 0; pad.style.height = "0px"; return; }
        distance = Math.min(90, Math.pow(raw, 0.82));
        pad.style.height = distance + "px";
        label.textContent = distance >= THRESHOLD ? T.webPullRelease : T.webPull;
    }, { passive: true });

    function end() {
        if (!pulling) return;
        pulling = false;
        pad.classList.remove("dragging");
        if (distance >= THRESHOLD && !busy) {
            busy = true;
            label.textContent = T.webPullBusy;
            pad.style.height = "34px";
            // **When the page is behind, the gesture reloads it.** Re-fetching data is right
            // almost always and exactly wrong in the one case where somebody is being told to
            // reload: pulling down is what a phone means by reloading, so without this the
            // notice could not be dismissed by the gesture that appears to dismiss it.
            if (Build.stale) { location.reload(); return; }
            Promise.resolve(api.refresh ? api.refresh() : null).then(function () {
                setTimeout(function () {
                    pad.style.height = "0px";
                    label.textContent = T.webPull;
                    busy = false;
                }, 260);
            });
        } else {
            pad.style.height = "0px";
        }
    }
    scroller.addEventListener("touchend", end, { passive: true });
    scroller.addEventListener("touchcancel", end, { passive: true });
})();

/* ---- the layout can change under us -------------------------------------- */

window.addEventListener("resize", function () {
    // Coming back to a desk with a transcript open on the phone layout would otherwise leave
    // the detail pane sitting off to one side of a two-column grid.
    if (!phone()) els.app.dataset.view = "list";
    else if (S.openId) els.app.dataset.view = "detail";
    // The marks are drawn for a particular scale factor, and a window moved between screens
    // has a new one.
    render();
    if (S.openId) renderTranscript();
});

/* ==========================================================================
   10. Go
   ========================================================================== */

// The wordmark wears the project's own mark, drawn by the same code the rows use — it comes
// from ~/.claude/project-icons.json in the app, and it is the one icon this page knows by heart.
var mark = {
    accent: "#d97757",
    cells: [".######.", ".#o##o#.", "########", ".##..##."].map(function (row) {
        return row.split("").map(function (ch) {
            return ch === "#" ? "#d97757" : (ch === "o" ? "#141416" : "#33201a");
        });
    })
};
drawIcon(els["brand-mark"], mark, 3);
drawIcon(els["door-mark"], mark, 3);

/**
 * Nothing is drawn until the interface has its words.
 *
 * The strings are the one thing the page cannot start without and cannot get for itself: they
 * decide what every label says, and repainting the furniture a moment after it has been read is
 * how a page comes to look like it changed its mind. So the first render waits, and the head of
 * this document holds the paint back while it does.
 *
 * Everything that can go wrong here ends the same way — the English already written into the
 * markup stands, and the page comes up. That is what it is in the document for.
 */
var booted = false;
function boot(data) {
    if (booted) return;
    booted = true;
    // Off first, and before anything that can throw: a page that is visible and half in English
    // is a page, and a page that is permanently invisible is not.
    document.documentElement.classList.remove("booting");
    try { applyStrings(data); } catch (e) { /* English, then */ }
    paintStatic();

    renderConn();
    // **Before the first paint, not after it.** The two screens that say "there is nothing here"
    // ask whether this wait is still running — see `listUnknown` — and a wait that has not been
    // started yet answers no. Started here, the first render draws neither of them.
    Waits.list.start();
    renderTranscript();
    render();
    // Read before the transport starts: `adoptToken` wipes the fragment when there is a token in
    // it, and a URL can carry both.
    routeTo(location.hash);
    api.start();
    Push.start();
}

if (location.protocol === "file:") {
    // A copy opened off a disk has no server to ask, and asking one that is not there is the
    // failed request in an otherwise clean console that teaches somebody to stop reading it.
    boot(null);
} else {
    // Not through `jsonFetch`: its own "could not reach Clawdline" is one of the strings being
    // fetched here, and a page cannot explain a failure in words it has not been given yet.
    // `no-store` because the answer depends on a header no cache is keyed on.
    fetch("/v1/strings", { cache: "no-store" })
        .then(function (res) { return res.ok ? res.json() : null; })
        .catch(function () { return null; })
        .then(boot);
    // And a page that is never drawn at all is worse than one drawn in English. A local answer
    // takes a millisecond; something in front of a tunnel on a bad connection takes longer, and
    // two seconds is well past the point where a dark rectangle stops reading as "loading".
    setTimeout(function () { boot(null); }, 2000);
}

// A page left open all day: "2m ago" is only true for a minute. The timestamps are rewritten in
// place rather than by rendering the transcript again — a re-render replaces the whole pane, and
// the reader would find themselves back at the top of it every minute for no reason they could see.
setInterval(function () {
    var stamps = els.tx.querySelectorAll("time[data-at]");
    for (var i = 0; i < stamps.length; i++) {
        stamps[i].textContent = clockOf(parseInt(stamps[i].getAttribute("data-at"), 10));
    }
}, 60000);
