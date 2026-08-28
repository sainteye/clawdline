/**
 * One active transcript read and one trailing demand per session. A request arriving while the
 * trailing read is in flight rearms that demand: those bytes were requested before the newest
 * revision existed, so exactly one final read must follow rather than swallowing the revision.
 */
export function createTranscriptRequests(fetchTranscript, accept) {
    var cycles = {};

    function run(cycle) {
        Promise.resolve().then(function () {
            return fetchTranscript(cycle.id);
        }).then(function (value) {
            finish(cycle, { value: value, error: null });
        }, function (error) {
            finish(cycle, { value: null, error: error });
        });
    }

    function finish(cycle, outcome) {
        if (cycle.needsTrailing) {
            cycle.needsTrailing = false;
            run(cycle);
            return;
        }
        delete cycles[cycle.id];
        accept(cycle.id, cycle.latestTicket, outcome, cycle.latestContext);
        cycle.resolve();
    }

    return function request(id, ticket, context) {
        var cycle = cycles[id];
        if (cycle) {
            cycle.latestTicket = ticket;
            cycle.latestContext = context;
            cycle.needsTrailing = true;
            return cycle.promise;
        }
        cycle = {
            id: id, latestTicket: ticket, latestContext: context, needsTrailing: false,
            resolve: null, promise: null
        };
        cycle.promise = new Promise(function (resolve) { cycle.resolve = resolve; });
        cycles[id] = cycle;
        run(cycle);
        return cycle.promise;
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
        load(state.id, state.revision, quiet);
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
                attempts: 0, inFlight: false, timer: null, rearmPending: false
            };
            states[id] = state;
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

    function settle(id, revision, succeeded) {
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
