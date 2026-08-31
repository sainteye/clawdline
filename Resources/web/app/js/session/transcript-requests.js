/**
 * One active transcript read and one trailing demand per session. A request arriving while the
 * trailing read is in flight rearms that demand: those bytes were requested before the newest
 * revision existed, so exactly one final read must follow rather than swallowing the revision.
 */
export function createTranscriptRequests(fetchTranscript, accept, options) {
    options = options || {};
    var cycles = {};
    var activeSession = null;
    var afterPaint = options.afterPaint || function (work) {
        if (typeof requestAnimationFrame === "function") {
            requestAnimationFrame(function () { requestAnimationFrame(work); });
        } else setTimeout(work, 0);
    };
    var phase = options.phase || function () {};

    function run(cycle, kind) {
        cycle.phase = kind;
        cycle.runningTicket = cycle.latestTicket;
        cycle.runningContext = cycle.latestContext;
        cycle.runningForeground = cycle.latestForeground;
        phase("request", { trailing: kind === "trailing" });
        // Calling the fetcher here is deliberate. The first network request must leave before
        // openSession draws the shell or starts optional info hydration; a promise microtask in
        // front of it inverted that priority on every Chat open.
        var result;
        try { result = fetchTranscript(cycle.id, { foreground: cycle.runningForeground }); }
        catch (error) { finish(cycle, { value: null, error: error }); return; }
        phase("client-issued", { trailing: kind === "trailing" });
        Promise.resolve(result).then(function (value) {
            phase("settled", { trailing: kind === "trailing" });
            finish(cycle, { value: value, error: null });
        }, function (error) {
            phase("failure", { trailing: kind === "trailing", code: error && error.code });
            finish(cycle, { value: null, error: error });
        });
    }

    function resolveCoveredWaiters(cycle) {
        var pending = [];
        cycle.waiters.forEach(function (waiter) {
            if (waiter.ticket <= cycle.runningTicket) {
                waiter.resolve({ accepted: true, ticket: cycle.runningTicket });
            } else pending.push(waiter);
        });
        cycle.waiters = pending;
    }

    function scheduleReplay(cycle) {
        if (cycle.replayScheduled) return;
        cycle.phase = "scheduled";
        cycle.replayScheduled = true;
        var token = ++cycle.scheduleToken;
        phase("post-paint-scheduled", {});
        afterPaint(function () {
            if (cycles[cycle.id] !== cycle || token !== cycle.scheduleToken) return;
            cycle.replayScheduled = false;
            // A stale session's scheduled refresh must not consume the newly opened session's
            // lane. Its demand remains paused and is rearmed if that session is opened again.
            if (activeSession !== null && activeSession !== cycle.id) {
                cycle.phase = "paused";
                return;
            }
            run(cycle, "active");
        });
    }

    function finish(cycle, outcome) {
        if (cycles[cycle.id] !== cycle) return;
        if (cycle.phase === "active" && cycle.latestTicket > cycle.runningTicket) {
            run(cycle, "trailing");
            return;
        }
        // A revision received while the trailing GET is in flight must not turn "one trailing"
        // into an unbounded quiet-wait loop. Paint this readable answer under the newest ticket,
        // but report the context it actually requested; one post-paint refresh then owns the
        // newest context.
        accept(cycle.id, cycle.latestTicket, outcome, cycle.runningContext);
        resolveCoveredWaiters(cycle);
        if (cycle.latestTicket > cycle.runningTicket) scheduleReplay(cycle);
        else delete cycles[cycle.id];
    }

    function request(id, ticket, context, demand) {
        demand = demand || {};
        var cycle = cycles[id];
        if (cycle) {
            // Ticket is the monotonic demand generation. A delayed callback can never replace a
            // newer context with the stale ticket/context it captured before paint.
            if (ticket > cycle.latestTicket) {
                cycle.latestTicket = ticket;
                cycle.latestContext = context;
                cycle.latestForeground = !!demand.foreground;
            } else if (ticket === cycle.latestTicket && demand.foreground) {
                cycle.latestForeground = true;
            }
            var existing = new Promise(function (resolve) {
                cycle.waiters.push({ ticket: ticket, resolve: resolve });
            });
            if (cycle.phase === "paused" &&
                (activeSession === null || activeSession === cycle.id)) scheduleReplay(cycle);
            return existing;
        }
        cycle = {
            id: id, latestTicket: ticket, latestContext: context,
            latestForeground: !!demand.foreground,
            runningTicket: ticket, runningContext: context,
            runningForeground: !!demand.foreground,
            phase: "active", replayScheduled: false, scheduleToken: 0, waiters: []
        };
        var promise = new Promise(function (resolve) {
            cycle.waiters.push({ ticket: ticket, resolve: resolve });
        });
        cycles[id] = cycle;
        run(cycle, "active");
        return promise;
    }
    request.activate = function (id) {
        activeSession = id || null;
        var cycle = id && cycles[id];
        if (cycle && cycle.phase === "paused") scheduleReplay(cycle);
    };
    return request;
}

/** Issue the first network read before the loading renderer can do DOM or layout work. */
export function beginTranscriptLoad(request, renderLoading) {
    var result = request();
    if (typeof renderLoading === "function") renderLoading();
    return result;
}

/** A generation-aware cache in which full Info always outranks the automatic summary tier. */
export function createTieredSessionFacts(readFull, readSummary, options) {
    options = options || {};
    var now = options.now || function () { return Date.now(); };
    var ttl = Math.max(0, Number(options.ttl) || 60000);
    var states = {};

    function stateFor(id) {
        return states[id] || (states[id] = {
            generation: 0, full: null, summary: null, fullPending: null, summaryPending: null
        });
    }
    function fresh(entry) { return !!(entry && now() - entry.at < ttl); }
    function invalidate(state, clear) {
        state.generation += 1;
        state.fullPending = null;
        state.summaryPending = null;
        if (clear) { state.full = null; state.summary = null; }
        return state.generation;
    }
    function read(id, tier, force) {
        if (!id) return Promise.resolve(null);
        var state = stateFor(id);
        if (force) invalidate(state, true);
        if (!force && fresh(state.full)) return Promise.resolve(state.full.data);
        if (tier === "summary" && !force && fresh(state.summary)) {
            return Promise.resolve(state.summary.data);
        }
        var pendingKey = tier + "Pending";
        if (state[pendingKey]) return state[pendingKey].promise;
        var reader = tier === "full" ? readFull : readSummary;
        if (typeof reader !== "function") return Promise.resolve(null);
        var generation = state.generation;
        var record = {};
        record.promise = Promise.resolve().then(function () { return reader(id); }).then(
            function (answer) {
                var data = (answer && answer.info) || null;
                if (state.generation !== generation || state[pendingKey] !== record) return null;
                state[pendingKey] = null;
                if (tier === "full") {
                    state.full = { tier: "full", generation: generation, data: data, at: now() };
                    state.summary = null;
                } else if (!state.full || state.full.generation !== generation) {
                    state.summary = {
                        tier: "summary", generation: generation, data: data, at: now()
                    };
                }
                return tier === "summary" && state.full ? state.full.data : data;
            }, function (error) {
                if (state.generation === generation && state[pendingKey] === record) {
                    state[pendingKey] = null;
                }
                throw error;
            });
        state[pendingKey] = record;
        return record.promise;
    }

    return {
        peek: function (id) {
            var state = states[id];
            return state && (state.full || state.summary)
                ? (state.full || state.summary).data : null;
        },
        fresh: function (id) { return !!(states[id] && fresh(states[id].full)); },
        tier: function (id) {
            var state = states[id];
            return state && state.full ? "full" : (state && state.summary ? "summary" : null);
        },
        drop: function (id) { if (id) invalidate(stateFor(id), true); },
        get: function (id, force) { return read(id, "full", !!force); },
        getSummary: function (id, force) { return read(id, "summary", !!force); },
        receiveFull: function (id, data) {
            if (!id || !data) return data || null;
            var state = stateFor(id);
            var generation = invalidate(state, false);
            state.full = { tier: "full", generation: generation, data: data, at: now() };
            state.summary = null;
            return data;
        }
    };
}

/**
 * Split renderer-owned units by both count and estimated source bytes. A single oversized unit
 * remains intact because splitting authored text would change its Markdown semantics; every
 * ordinary unit is bounded, and the renderer yields between the returned chunks.
 */
export function planTranscriptRenderChunks(items, estimate, options) {
    options = options || {};
    var byteBudget = Math.max(1, Number(options.byteBudget) || 128 * 1024);
    var itemBudget = Math.max(1, Number(options.itemBudget) || 12);
    var chunks = [], chunk = [], bytes = 0;
    (items || []).forEach(function (item) {
        var size = Math.max(0, Number(estimate(item)) || 0);
        if (chunk.length && (chunk.length >= itemBudget || bytes + size > byteBudget)) {
            chunks.push(chunk); chunk = []; bytes = 0;
        }
        chunk.push(item); bytes += size;
    });
    if (chunk.length) chunks.push(chunk);
    return chunks;
}

// Acceptance budgets, kept beside the scheduling seams the focused browser test exercises.
// Server/runtime landing evidence supplies the measured p95 values; these are the thresholds.
export var TRANSCRIPT_LATENCY_BUDGETS = Object.freeze({
    healthyLocalTTFBP95Ms: 250,
    ordinaryResponseToMeaningfulPaintP95Ms: 100,
    largeRenderTaskMaxMs: 50
});

/**
 * The production scheduler behind `renderTranscript`. Its inserted-content adapter is the DOM
 * seam: this function owns ordering, insertion direction, scroll compensation, cancellation,
 * the meaningful-paint image gate and real per-task duration/over-budget telemetry.
 */
export function scheduleTranscriptRender(options) {
    options = options || {};
    var chunks = (options.chunks || []).slice();
    var newestFirst = !!options.newestFirst;
    var isCurrent = options.isCurrent || function () { return true; };
    var schedule = options.schedule || function (work) { setTimeout(work, 0); };
    var afterPaint = options.afterPaint || function (work) {
        if (typeof requestAnimationFrame === "function") {
            requestAnimationFrame(function () { requestAnimationFrame(work); });
        } else schedule(work);
    };
    var clock = options.clock || function () {
        return typeof performance !== "undefined" && performance.now
            ? performance.now() : Date.now();
    };
    var insert = options.insert || function () { return {}; };
    var hydrate = options.hydrate || function () {};
    var adjustScroll = options.adjustScroll || function () {};
    var note = options.note || function () {};
    var meaningful = options.meaningful || function () {};
    var complete = options.complete || function () {};
    var deferredImages = [];
    var taskCount = 0;

    function take() { return newestFirst ? chunks.shift() : chunks.pop(); }
    function perform(first) {
        if (!isCurrent() || !chunks.length) return false;
        var chunk = take();
        var prepend = !first && !newestFirst;
        var started = clock();
        var result = insert(chunk, { first: first, prepend: prepend }) || {};
        var duration = Math.max(0, clock() - started);
        taskCount += 1;
        note("render.task", {
            first: first, durationMs: Math.round(duration * 10) / 10,
            overBudget: duration > TRANSCRIPT_LATENCY_BUDGETS.largeRenderTaskMaxMs
        });
        if (prepend && Number(result.heightDelta) > 0) adjustScroll(Number(result.heightDelta));
        var images = Array.from(result.images || []);
        if (first) deferredImages.push.apply(deferredImages, images);
        else if (images.length) hydrate(images);
        note("render.chunk", {
            first: first, entries: Number(result.entries) || chunk.length,
            remaining: chunks.length
        });
        return true;
    }

    function continueRendering() {
        if (!isCurrent() || !chunks.length) return;
        perform(false);
        if (chunks.length) schedule(continueRendering);
        else complete({ tasks: taskCount });
    }

    if (!chunks.length || !perform(true)) return { started: false };
    afterPaint(function () {
        if (!isCurrent()) return;
        var imageCount = deferredImages.length;
        note("meaningful-paint", {
            entries: Number(options.entryCount) || 0,
            chunked: (options.chunks || []).length > 1,
            deferredImages: imageCount
        });
        meaningful();
        if (imageCount) hydrate(deferredImages.splice(0));
        if (chunks.length) schedule(continueRendering);
        else complete({ tasks: taskCount });
    });
    return { started: true };
}

/**
 * Translate the local file-revision lane into transcript demand without confusing it with the
 * session snapshot revision. The two clocks can move independently: a JSONL append need not
 * change state, line or label, and an inventory refresh need not append a byte.
 */
export function createTranscriptEventRouter(currentSessionID, observe, reconnect) {
    return function route(event) {
        if (!event) return false;
        if (event.type === "hello") {
            var reconnectID = currentSessionID();
            if (!reconnectID || typeof reconnect !== "function") return false;
            reconnect(reconnectID);
            return true;
        }
        if (event.type !== "transcript-revision") return false;
        var data = event.data || {};
        var openID = currentSessionID();
        if (!openID || data.id !== openID || typeof data.signature !== "string" ||
            !data.signature) return false;
        observe(data.id, data.signature);
        return true;
    };
}

/**
 * Keep the newest session snapshot revision separate from the revision whose transcript GET
 * actually succeeded. A transient final GET failure gets a small recovery budget; repeated SSE
 * snapshots for the same revision neither consume that budget nor create parallel reads.
 */
export function createTranscriptRevisionObserver(load, options) {
    options = options || {};
    var configuredAttempts = Number(options.maxAttempts);
    var configuredDelay = Number(options.retryDelay);
    var maxAttempts = Number.isFinite(configuredAttempts)
        ? Math.max(1, Math.floor(configuredAttempts)) : 3;
    var retryDelay = Number.isFinite(configuredDelay) ? Math.max(0, configuredDelay) : 250;
    var schedule = options.schedule || function (fn, delay) { return setTimeout(fn, delay); };
    var cancel = options.cancel || function (timer) { clearTimeout(timer); };
    var states = {};

    function launch(state, quiet) {
        if (states[state.id] !== state || state.inFlight || state.timer ||
            state.attempts >= maxAttempts || state.observedRevision === state.revision) return;
        state.inFlight = true;
        state.attempts += 1;
        load(state.id, state.revision, quiet, { foreground: state.foreground });
    }

    function observe(id, revision, quiet) {
        demand(id, revision, quiet, false);
    }

    function rearm(id, revision, quiet) {
        demand(id, revision, quiet, true);
    }

    function demand(id, revision, quiet, isReconnect) {
        if (!id || revision == null) return;
        var state = states[id];
        if (!state || state.revision !== revision) {
            if (state && state.timer) cancel(state.timer);
            state = {
                id: id, revision: revision,
                // This is observation of this occurrence, not a set of revision strings seen in
                // history. Revisions are non-monotonic, so A after A→B is fresh demand.
                observedRevision: null,
                attempts: 0, busyRetries: 0, foreground: quiet === false,
                inFlight: false, timer: null, rearmPending: false
            };
            states[id] = state;
        } else if (quiet === false) {
            state.foreground = true;
        } else if (isReconnect && state.observedRevision !== revision &&
                   state.attempts >= maxAttempts) {
            // A burst stops itself after maxAttempts. Only an explicit transport reconnect/new
            // demand may open the next burst; ordinary repeated snapshots keep the exhausted
            // state and therefore cannot make a fetch loop. If the last GET is still active,
            // remember the boundary and start the new burst only after it settles.
            if (state.inFlight) state.rearmPending = true;
            else state.attempts = 0;
        }
        launch(state, quiet !== false);
    }

    function settle(id, revision, succeeded, error) {
        var state = states[id];
        if (!state || state.revision !== revision) return;
        state.inFlight = false;
        if (succeeded) {
            state.observedRevision = revision;
            state.rearmPending = false;
            if (state.timer) cancel(state.timer);
            state.timer = null;
            return;
        }
        if (error && error.code === "transcript_busy" && state.busyRetries < 8) {
            state.busyRetries += 1;
            // Capacity debt is not a failed content read. Keep the ordinary retry budget intact
            // and wait for the server's drain receipt instead of exhausting three short retries.
            state.attempts = Math.max(0, state.attempts - 1);
            var busyDelay = Math.max(
                50, Number(error.retryAfter || error.retry_after) * 1000 || 500);
            var busyTimer = schedule(function () {
                if (states[id] !== state || state.timer !== busyTimer) return;
                state.timer = null;
                launch(state, true);
            }, busyDelay);
            state.timer = busyTimer;
            return;
        }
        if (state.rearmPending) {
            state.rearmPending = false;
            state.attempts = 0;
            launch(state, true);
            return;
        }
        if (state.observedRevision === revision || state.attempts >= maxAttempts || state.timer) {
            return;
        }
        var timer = schedule(function () {
            if (states[id] !== state || state.timer !== timer) return;
            state.timer = null;
            launch(state, true);
        }, retryDelay * state.attempts);
        state.timer = timer;
    }

    function stop(id) {
        var state = states[id];
        if (!state) return;
        if (state.timer) cancel(state.timer);
        delete states[id];
    }

    return { observe: observe, rearm: rearm, settle: settle, stop: stop };
}
