/**
 * One terminal capture at a time, with the next read scheduled only after settlement. Switching
 * sessions invalidates the old answer but does not overlap Apple Events; the new session becomes
 * the one trailing demand and starts as soon as the old request releases the lane.
 */
export function createLivePreviewFollower(fetchPreview, accept, options) {
    options = options || {};
    var delay = Number.isFinite(Number(options.delay)) ? Math.max(100, Number(options.delay)) : 700;
    var retryDelay = Number.isFinite(Number(options.retryDelay))
        ? Math.max(delay, Number(options.retryDelay)) : 1400;
    var schedule = options.schedule || function (fn, wait) { return setTimeout(fn, wait); };
    var cancel = options.cancel || function (timer) { clearTimeout(timer); };
    var wanted = null;
    var generation = 0;
    var inFlight = false;
    var timer = null;

    function launch() {
        if (!wanted || inFlight) return;
        var id = wanted;
        var turn = generation;
        inFlight = true;
        Promise.resolve().then(function () { return fetchPreview(id); }).then(function (value) {
            settle(id, turn, { value: value, error: null });
        }, function (error) {
            settle(id, turn, { value: null, error: error });
        });
    }

    function settle(id, turn, outcome) {
        inFlight = false;
        if (!wanted) return;
        if (turn !== generation || id !== wanted) {
            launch();
            return;
        }
        accept(id, outcome);
        var heldGeneration = generation;
        timer = schedule(function () {
            if (timer == null || generation !== heldGeneration || wanted !== id) return;
            timer = null;
            launch();
        }, outcome.error ? retryDelay : delay);
    }

    function start(id) {
        if (!id || wanted === id) return;
        wanted = id;
        generation += 1;
        if (timer != null) { cancel(timer); timer = null; }
        launch();
    }

    function stop() {
        wanted = null;
        generation += 1;
        if (timer != null) { cancel(timer); timer = null; }
    }

    return { start: start, stop: stop };
}
