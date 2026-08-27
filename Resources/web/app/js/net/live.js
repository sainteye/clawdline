import { els } from "../core/dom.js";
import { toast, uuid } from "../core/util.js";
import { handlers } from "./handlers.js";
import { adoptToken, jsonFetch, post } from "./fetch.js";
import { Door } from "../door/door.js";

export var Live = {
    es: null,
    attempt: 0,
    timer: null,
    countdown: null,
    sessionProbe: null,
    sessionRevision: 0,

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
            self.receiveSessions(d);
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
        this.sessionProbe = null;
        this.sessionRevision += 1;
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
                self.receiveSessions(d);
                // A payload that parsed is the only proof the connection actually works;
                // onopen fires for a socket that a proxy may still be holding open with nothing
                // behind it. A guarded empty frame still proves the feed, even though a separate
                // reading must confirm it before it can close an open chat.
                self.attempt = 0;
                handlers.conn("live");
            } catch (e) { }
        });
        // Dispatched work. Its own event because it moves on its own clock — a task is briefed
        // and finishes without the session list changing at all — and an app that has never
        // heard of it simply never sends one.
        es.addEventListener("orchestrator", function (ev) {
            try { handlers.tasks(JSON.parse(ev.data).tasks); } catch (e) { }
        });
        es.onerror = function () {
            // EventSource has a reconnect policy of its own, and it is not one that can be seen
            // or slowed down. Closing it and coming back on our own terms is what makes the
            // backoff visible in the header instead of being a silent loop in the browser.
            es.close();
            if (self.es === es) self.es = null;
            self.retry();
        };
    },

    /**
     * Accept a whole session inventory, or ask once for evidence newer than a destructive empty
     * frame. `/v1/sessions` reads the watcher's published state, so a same-generation response is
     * explicitly not independent confirmation. A complete scan, or per-tty process proof carried
     * as `emptyAuthoritative`, closes a real session without an arbitrary grace period.
     */
    receiveSessions: function (data) {
        data = data || {};
        var scan = data.scan || {};
        if (handlers.sessions(data.sessions, data.at, scan) !== false) {
            this.sessionRevision += 1;
            this.sessionProbe = null;
            return Promise.resolve(true);
        }

        if (this.sessionProbe) return this.sessionProbe.promise;
        var self = this;
        var marker = {
            revision: this.sessionRevision,
            generation: Number(scan.generation) || 0,
            promise: null
        };
        this.sessionProbe = marker;
        marker.promise = jsonFetch("/v1/sessions", { cache: "no-store" }).then(function (fresh) {
            if (self.sessionProbe !== marker || self.sessionRevision !== marker.revision) return false;
            self.sessionProbe = null;
            var freshScan = fresh.scan || {};
            var freshGeneration = Number(freshScan.generation) || 0;
            if ((fresh.sessions || []).length === 0 &&
                (freshScan.emptyAuthoritative !== true || freshGeneration <= marker.generation)) {
                return false;
            }
            return self.receiveSessions(fresh);
        }).catch(function () {
            if (self.sessionProbe === marker) self.sessionProbe = null;
            return false;
        });
        return marker.promise;
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
        return jsonFetch("/v1/sessions").then(function (d) { return self.receiveSessions(d); })
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

    /// One of that session's background commands: the tail of the file it is printing into.
    /// Not a transcript — a command has no turns — so this answers text and whether it has ended.
    shell: function (id, shellId) {
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/shells/"
                         + encodeURIComponent(shellId));
    },

    /// Stop one. **The second route on this server that destroys something**, after `end`, and
    /// it takes the same key so that a retry of *this* press is not a second signal.
    killShell: function (id, shellId) {
        return jsonFetch("/v1/sessions/" + encodeURIComponent(id) + "/shells/"
                         + encodeURIComponent(shellId) + "/kill",
                         post({}, { "Idempotency-Key": uuid() }));
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

    /// Words said out loud, turned back into words.
    ///
    /// **Not a session route, and not a send.** The Mac transcribes and answers with the text;
    /// what happens to it afterwards is the composer's business, and what the composer does is
    /// put it in the box. Nothing on this path can reach a terminal, which is what makes a
    /// dictation that heard the wrong thing a typo rather than an incident.
    ///
    /// The audio goes up as base64 of little-endian Int16 mono at 16 kHz, because whisper.cpp
    /// takes 16 kHz and nothing else — the resampling is done in the browser rather than on the
    /// Mac, which has no ffmpeg and could not open an Opus file if it wanted to. A minute of it
    /// is about 2.6MB encoded, against a body limit of twenty; see the ceiling in `voice.js`.
    /// The key is minted once per recording, so a retry of *this* request is not a second read.
    voice: function (audio, rate) {
        return jsonFetch("/v1/voice", post({ audio: audio, rate: rate },
                                           { "Idempotency-Key": uuid() }));
    },

    /// One sentence, turned into a draft session: which project, which assistant, what to say
    /// first. Gated exactly like `voice` above — the write switch, the `send` capability, a
    /// key — because thinking about it spends this Mac's CPU and the speaker's own model quota,
    /// which is the same reason dictation is gated rather than left as a read. It is also the
    /// slowest ordinary request this page makes: 4.6-5.1 seconds measured, so the caller is the
    /// one that has to say so honestly rather than sit on a sheet that looks stuck.
    intents: function (text) {
        return jsonFetch("/v1/intents", post({ text: text }, { "Idempotency-Key": uuid() }));
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

    /// The id, the assistant and now the model are the whole request, and **all three are in the
    /// path**. There is no body on this route — not "an optional body", none is read — so there
    /// is nothing this page could send that would widen what gets started, and the command at
    /// the other end is a literal off a two-case enum plus a name `StartPoints.start` checks the
    /// same way. An assistant or a model this page invented is a 404 at the Mac rather than a
    /// string that reaches a shell — never a silent fallback to whatever the default would have
    /// been. The key is minted once per press, which is what makes a retry of *this* request the
    /// same start rather than a second tab.
    ///
    /// `model` is a fourth path segment, parsed the way the third already is, so it can only
    /// appear once the third does — a bare model with no assistant would land in the assistant's
    /// own slot and be read as one. `input/command.js` never sends one without the other; a
    /// falsy `model` (the planner named none, or the chosen assistant is not Claude) leaves the
    /// path exactly as it was before this segment existed.
    ///
    /// Named for what it starts. `start` on this object is the transport's own — the one `boot`
    /// calls to open the stream — and a second one would have quietly replaced it.
    startPlace: function (id, assistant, model) {
        var path = "/v1/places/" + encodeURIComponent(id) + "/start";
        if (assistant || model) path += "/" + encodeURIComponent(assistant || "claude");
        if (model) path += "/" + encodeURIComponent(model);
        return jsonFetch(path, post({}, { "Idempotency-Key": uuid() }));
    },

    /// The conversations one assistant has already recorded in a place. Reading, not starting —
    /// it discloses titles held in a directory this token could already see. Omitting the
    /// assistant keeps the original Claude route for older callers.
    pastSessions: function (id, assistant) {
        var path = "/v1/places/" + encodeURIComponent(id) + "/sessions";
        if (assistant) path += "/" + encodeURIComponent(assistant);
        return jsonFetch(path);
    },

    /// Pick one of them back up. **Both ids are in the path** — the same shape as `startPlace`
    /// and for the same reason: there is no body on this route either, so there is nothing this
    /// page could send that would widen what gets run. The conversation is checked at the Mac
    /// for being a UUID *and* for being one it just listed for that directory; anything else is
    /// a 404 there rather than a string on a command line.
    resumePlace: function (id, session, assistant) {
        var path = "/v1/places/" + encodeURIComponent(id) + "/resume/";
        if (assistant) path += encodeURIComponent(assistant) + "/";
        path += encodeURIComponent(session);
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

/* ---- schedules: reading is ambient, making one is gated ------------------
   Kept outside the transport object above so the two permissions stay visible at a glance:
   listing needs nothing but pairing, the same as everything else on `Live`. Making one goes
   through `writeGate` exactly like `voice` and `intents` below it — the write switch, the `send`
   capability, an Idempotency-Key — and never the orchestrator token, which is local-only and
   could not reach a phone. Neither route can make an existing schedule *run* early; that button
   does not exist anywhere on this page.
   -------------------------------------------------------------------------- */
Live.schedules = function () { return jsonFetch("/v1/orchestrator/schedules"); };

/// One schedule, made. `input/schedule.js` is the only caller and the only place that builds
/// `schedule` — see the field list on `POST /v1/orchestrator/schedules` in the plan this task
/// came with. The key is minted once per press, so a retry of *this* request does not make two.
Live.createSchedule = function (schedule) {
    return jsonFetch("/v1/orchestrator/schedules", post(schedule, { "Idempotency-Key": uuid() }));
};

/// One schedule, in full — the task template and every field the list route leaves out. Read
/// level, same door as `schedules` above: a paired device without `send` can still open a row
/// and look, and only the two writes below it ask for more.
Live.schedule = function (id) {
    return jsonFetch("/v1/orchestrator/schedules/" + encodeURIComponent(id));
};

/// Changing one already made. Same body `createSchedule` sends, same gate, same reason:
/// `Orchestrator.updateSchedule(id:from:places:)` hands it to the same parser and only lets a
/// file that reads back correctly survive the request. `schedule_id` and `created_at` are not on
/// the body's allowed field list — the id is in the path, and the Mac is the one keeping the
/// date. `post()` builds the body PATCH and POST share; only the verb differs.
Live.updateSchedule = function (id, schedule) {
    var opts = post(schedule, { "Idempotency-Key": uuid() });
    opts.method = "PATCH";
    return jsonFetch("/v1/orchestrator/schedules/" + encodeURIComponent(id), opts);
};

/// Removing one. There is no body to send — the id in the path is the whole request, the same
/// shape `killShell` and `end` above already take for "there is nothing this page could send
/// that would widen what happens" — and no undo route anywhere on this page once it lands.
Live.deleteSchedule = function (id) {
    return jsonFetch("/v1/orchestrator/schedules/" + encodeURIComponent(id),
                     { method: "DELETE", headers: { "Idempotency-Key": uuid() } });
};
