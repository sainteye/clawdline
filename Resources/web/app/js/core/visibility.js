/* --------------------------------------------------------------------------
   What this page does while nobody can see it: nothing.

   A phone that has put the console in the background is still holding it, and every clock this
   page starts keeps the radio and the CPU awake for a screen nobody is reading. Measured on
   2026-09-15 against the hosted console, the page kept redrawing spinners, polling a terminal
   and re-reading session facts with the phone locked. Those are all the page's own clocks — the
   transport's socket is a separate question and lives in `net/cloud-*.js`.

   Two shapes cover every one of them:

   - **A repeating clock** (`createVisibleInterval`) stops its timer when the page is hidden and
     starts it again when the page comes back, running its work once on the way back if a tick
     was missed. "Catch up once" rather than replaying every missed tick: a minute's relabel
     that missed ten minutes needs to happen once, not ten times.
   - **A batch of redraws** (`createFrameCoalescer`) turns any number of requests into one run in
     the next animation frame, and while the page is hidden holds them as one pending run for the
     moment it becomes visible.

   Both take an environment so a test can drive hidden/visible and the clocks by hand. The page
   always passes none and gets the real `document`, timers and `requestAnimationFrame`.
   -------------------------------------------------------------------------- */

function realDocument() {
    return typeof document !== "undefined" && document ? document : null;
}

/** Whether the page is hidden right now. A page with no document (a worker, a test) is not. */
export function pageHidden() {
    var doc = realDocument();
    return !!(doc && doc.hidden === true);
}

/**
 * Run `work` now if the page is visible, otherwise once when it next becomes visible. Several
 * calls while hidden run `work` as many times; callers that want one should keep their own flag.
 */
export function whenVisible(work, overrides) {
    var env = visibilityEnvironment(overrides);
    if (!env.hidden()) { work(); return; }
    var off = env.subscribe(function () {
        if (env.hidden()) return;
        off();
        work();
    });
}

/**
 * The seams both shapes use. Every member may be overridden; the defaults are the browser's.
 * `subscribe(listener)` calls `listener` on every visibility change and returns its own undo.
 */
export function visibilityEnvironment(overrides) {
    overrides = overrides || {};
    var env = {
        hidden: overrides.hidden || pageHidden,
        subscribe: overrides.subscribe || function (listener) {
            var doc = realDocument();
            if (!doc || typeof doc.addEventListener !== "function") return function () {};
            var handler = function () { listener(); };
            doc.addEventListener("visibilitychange", handler);
            return function () {
                if (typeof doc.removeEventListener === "function") {
                    doc.removeEventListener("visibilitychange", handler);
                }
            };
        },
        now: overrides.now || function () { return Date.now(); },
        setInterval: overrides.setInterval || function (work, ms) { return setInterval(work, ms); },
        clearInterval: overrides.clearInterval || function (timer) { clearInterval(timer); },
        requestFrame: overrides.requestFrame || function (work) {
            if (typeof requestAnimationFrame === "function") return requestAnimationFrame(work);
            return setTimeout(work, 16);
        },
        setTimeout: overrides.setTimeout || function (work, ms) { return setTimeout(work, ms); },
        clearTimeout: overrides.clearTimeout || function (timer) { clearTimeout(timer); }
    };
    return env;
}

/**
 * A `setInterval` that does no work while the page is hidden.
 *
 * Hidden means the timer itself is gone, not merely a tick that returns early: an armed interval
 * wakes a backgrounded phone just to find out it has nothing to do. Coming back runs `work` once
 * if at least one interval elapsed since it last ran (`catchUp: false` skips that — a spinner has
 * nothing to catch up on), then arms the timer again.
 */
export function createVisibleInterval(work, intervalMs, options) {
    options = options || {};
    var env = visibilityEnvironment(options.environment);
    var ms = Math.max(1, Number(intervalMs) || 1);
    var catchUp = options.catchUp !== false;
    var timer = null;
    var running = false;
    var lastRunAt = 0;
    var unsubscribe = null;

    function disarm() {
        if (timer === null) return;
        env.clearInterval(timer);
        timer = null;
    }
    function arm() {
        if (timer !== null || !running) return;
        timer = env.setInterval(tick, ms);
    }
    function run() {
        lastRunAt = env.now();
        work();
    }
    function tick() {
        // A tick already queued when the page went away must not do the work it was queued for.
        if (env.hidden()) { disarm(); return; }
        run();
    }
    function changed() {
        if (!running) return;
        if (env.hidden()) { disarm(); return; }
        if (timer !== null) return;
        if (catchUp && env.now() - lastRunAt >= ms) run();
        arm();
    }

    return {
        start: function () {
            if (running) return;
            running = true;
            lastRunAt = env.now();
            unsubscribe = env.subscribe(changed);
            if (!env.hidden()) arm();
        },
        stop: function () {
            running = false;
            disarm();
            if (unsubscribe) { unsubscribe(); unsubscribe = null; }
        },
        running: function () { return running; },
        /** Whether a real timer is armed at this moment — false while hidden. For the tests. */
        armed: function () { return timer !== null; }
    };
}

/**
 * How long a visible page may go without an animation frame before a pending draw runs anyway.
 * A page that says it is visible and still gets no frames exists — a backgrounded automation tab,
 * a web view whose window is covered — and a list that never draws there is worse than one
 * drawn a second late. The timer exists only while a draw is owed.
 */
export var FRAME_FALLBACK_MS = 1000;

/**
 * Any number of `request()` calls, one `work()` in the next animation frame.
 *
 * While the page is hidden nothing is scheduled at all; the requests collapse into one run that
 * happens when the page is visible again. The state behind the work is expected to be current
 * already — this only decides *when it is drawn*, so a run is never lost and never repeated.
 */
export function createFrameCoalescer(work, options) {
    options = options || {};
    var env = visibilityEnvironment(options.environment);
    var scheduled = false;
    var waitingForVisible = false;
    var generation = 0;
    var fallback = null;

    function run(mine) {
        if (!scheduled || mine !== generation) return;
        scheduled = false;
        if (fallback !== null) { env.clearTimeout(fallback); fallback = null; }
        if (env.hidden()) { holdUntilVisible(); return; }
        work();
    }
    function holdUntilVisible() {
        if (waitingForVisible) return;
        waitingForVisible = true;
        var off = env.subscribe(function () {
            if (env.hidden()) return;
            off();
            waitingForVisible = false;
            request();
        });
    }
    function request() {
        if (scheduled || waitingForVisible) return;
        if (env.hidden()) { holdUntilVisible(); return; }
        scheduled = true;
        var mine = ++generation;
        env.requestFrame(function () { run(mine); });
        fallback = env.setTimeout(function () { fallback = null; run(mine); }, FRAME_FALLBACK_MS);
    }

    return {
        request: request,
        pending: function () { return scheduled || waitingForVisible; }
    };
}
