/* Expiring image artifacts in a transcript. Data decides state; markup never comes from it. */

const artifactID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const artifactTileBindings = new WeakMap();

function validArtifact(a) {
    return typeof a.id === "string" && artifactID.test(a.id) &&
        a.media_type === "image/png" && Number.isSafeInteger(a.byte_count) && a.byte_count > 0 &&
        Number.isSafeInteger(a.width) && a.width > 0 &&
        Number.isSafeInteger(a.height) && a.height > 0 &&
        Number.isSafeInteger(a.expires_at) && a.expires_at > 0;
}

/** The initial client state for one closed artifact reference. */
export function artifactPresentation(artifact, now = Math.floor(Date.now() / 1000)) {
    var a = artifact || {};
    if (!validArtifact(a) || a.expires_at <= now) return { state: "expired" };
    return {
        state: "loading",
        // Relative and same-origin on purpose, and only where the origin is the Mac. That
        // sentence used to end "localhost today and clawdline.com later have the same contract",
        // and the second half of it was not true: on the cloud path this page is served by the
        // hosted console, which has no artifact route and cannot have one, because the Mac is not
        // reachable from it. A transport that cannot be asked over HTTP supplies its own
        // `source` to `connectArtifactTile` and this URL is never built.
        url: "/v1/artifacts/images/" + encodeURIComponent(a.id),
        width: a.width,
        height: a.height
    };
}

/** The identity of DOM state which is safe to carry across a transcript redraw. */
function artifactReconciliationKey(artifact, now) {
    var a = artifact || {};
    if (!validArtifact(a)) return null;
    return [
        a.id, a.media_type, a.byte_count, a.width, a.height, a.expires_at,
        a.expires_at <= now ? "expired" : "available"
    ].join("\u001f");
}

/**
 * Move unchanged, already-connected tiles into freshly parsed transcript markup.
 *
 * Existing bindings live in a WeakMap: artifact data never enters markup, and removing a
 * message leaves no registry holding its detached image alive. The caller hydrates only `fresh`
 * before installing the new fragment, then calls `restoreFocus` after installation.
 */
export function reconcileArtifactTiles(currentTiles, nextTiles, artifacts, options = {}) {
    var now = options.now == null ? Math.floor(Date.now() / 1000) : options.now;
    var available = new Map();
    var active = options.activeElement || null;
    var focus = null;

    Array.from(currentTiles || []).forEach(function (tile) {
        var binding = artifactTileBindings.get(tile);
        if (!binding || !binding.key) return;
        var matches = available.get(binding.key) || [];
        matches.push(tile);
        available.set(binding.key, matches);
    });

    var fresh = [];
    var reused = [];
    var tiles = [];
    var offered = [];
    available.forEach(function (matches) { matches.forEach(function (t) { offered.push(t); }); });
    Array.from(nextTiles || []).forEach(function (placeholder) {
        var slot = Number(placeholder.dataset.artifactSlot);
        var artifact = Number.isSafeInteger(slot) ? artifacts[slot] : null;
        var key = artifactReconciliationKey(artifact, now);
        var matches = key ? available.get(key) : null;
        var tile = matches && matches.length ? matches.shift() : null;
        if (!tile) {
            fresh.push(placeholder);
            tiles.push(placeholder);
            return;
        }
        tile.dataset.artifactSlot = String(slot);
        placeholder.replaceWith(tile);
        reused.push(tile);
        tiles.push(tile);
        if (active && (active === tile || (tile.contains && tile.contains(active)))) focus = active;
    });

    // A carried picture is bytes this page is holding, not a URL the browser will forget. A tile
    // the new markup had no use for is about to be removed by the caller, and the object URL
    // pointing at its megabytes would outlive it all the way to a page reload — so the tiles that
    // were offered and not taken hand their bytes back here, where "not reused" is known.
    var dropped = offered.filter(function (tile) { return reused.indexOf(tile) === -1; });
    dropped.forEach(releaseArtifactTile);

    return {
        fresh: fresh,
        reused: reused,
        tiles: tiles,
        dropped: dropped,
        restoreFocus: function () {
            if (focus && focus.focus) focus.focus({ preventScroll: true });
        }
    };
}

/** Hand back whatever bytes a tile was holding. Safe to call twice and on a tile that held none. */
export function releaseArtifactTile(tile) {
    var binding = artifactTileBindings.get(tile);
    if (!binding || !binding.release) return;
    var release = binding.release;
    binding.release = null;
    release();
}

/**
 * Connect a static, locally-authored tile to artifact data using DOM properties only.
 *
 * Two ways in, and which one is used is the transport's answer rather than this file's guess.
 * Without `options.source` the tile asks its own origin for the bytes, which is the direct path
 * and is unchanged to the character. With one, the transport hands the picture over itself and
 * this tile renders whatever URL it is given — and, crucially, is told *why* when it is given
 * none, so a picture that could not cross says so in a sentence instead of leaving the browser
 * to draw its broken-image icon and let the reader think they broke something.
 */
export function connectArtifactTile(tile, artifact, options = {}) {
    var image = tile.querySelector(".message-image");
    var status = tile.querySelector(".message-image-state");
    var now = options.now == null ? Math.floor(Date.now() / 1000) : options.now;
    var presentation = artifactPresentation(artifact, now);
    var binding = { key: artifactReconciliationKey(artifact, now), release: null };
    artifactTileBindings.set(tile, binding);

    function say(state, words) {
        releaseArtifactTile(tile);
        tile.dataset.imageState = state;
        tile.disabled = true;
        image.hidden = true;
        image.removeAttribute("src");
        status.hidden = false;
        status.setAttribute("role", "status");
        status.textContent = words;
    }

    function expire() {
        say("expired", options.expiredLabel || "Image expired");
    }

    /**
     * The other end of a picture that did not arrive, and the reason this option exists.
     *
     * `describeFailure` is given the transport's own code — `image_too_large_for_cloud`,
     * `cloud_read_needs_send_prompt`, `cloud_read_timeout`, `offline` — and answers with a
     * sentence in the reader's language. A code with no sentence behind it falls back to the
     * expired wording rather than printing itself: an English identifier on a page in thirteen
     * other languages is its own kind of broken image.
     */
    function refuse(error) {
        var code = error && error.code ? String(error.code) : "";
        var words = options.describeFailure ? options.describeFailure(code, artifact) : "";
        if (!words) { expire(); return; }
        say("unavailable", words);
    }

    if (presentation.state === "expired") {
        expire();
        return { presentation: presentation, expire: expire, refuse: refuse };
    }

    tile.dataset.imageState = "loading";
    tile.disabled = true;
    image.hidden = true;
    status.hidden = false;
    status.setAttribute("role", "status");
    status.textContent = options.loadingLabel || "Loading…";
    image.addEventListener("load", function () {
        tile.dataset.imageState = "live";
        tile.disabled = false;
        image.hidden = false;
        status.hidden = true;
    });
    // The endpoint types 404 and 410, but `<img>` does not expose the status. Every request
    // failure therefore takes the same conservative, unmistakable visible branch.
    image.addEventListener("error", expire);
    tile.addEventListener("click", function () {
        if (tile.dataset.imageState === "live" && options.open) {
            options.open(tile, image.src, expire);
        }
    });
    if (typeof options.source !== "function") {
        image.src = presentation.url;
        return { presentation: presentation, expire: expire, refuse: refuse };
    }
    // The tile is already announcing "Loading…" and the request is a round trip through a relay,
    // so nothing here waits for it. A tile that has been replaced by the time the bytes land has
    // had its binding overwritten; releasing against the tile rather than closing over the URL is
    // what keeps that from freeing somebody else's picture.
    var carried = Promise.resolve()
        .then(function () { return options.source(artifact); })
        .then(function (delivery) {
            if (!delivery || typeof delivery.url !== "string" || !delivery.url) {
                throw Object.assign(new Error("no image url"), { code: "bad_payload" });
            }
            if (artifactTileBindings.get(tile) !== binding) {
                if (delivery.release) delivery.release();
                return;
            }
            binding.release = delivery.release || null;
            image.src = delivery.url;
        })
        .catch(function (error) {
            if (artifactTileBindings.get(tile) !== binding) return;
            refuse(error);
        });
    return { presentation: presentation, expire: expire, refuse: refuse, carried: carried };
}

/* ------------------------------------------------------------------- zooming */

// How far into a picture the lightbox will go. The floor is 1 by construction: the stylesheet has
// already fitted the picture to its frame, so scale 1 *is* "fits the screen" and there is nothing
// below it worth showing. The ceiling is the end with a decision in it, and it is taken from the
// source's own pixels — the point of enlarging a phone screenshot is to read the two thirds of it
// the frame had no room for. Bounded at both ends because that ratio is not always useful: a
// picture no larger than its frame would cap at 1x and never zoom at all, and a 6000px photo would
// cap somewhere a finger cannot find its way back from.
var ZOOM_FIT = 1;
var ZOOM_CEILING_MIN = 4;
var ZOOM_CEILING_MAX = 8;
// Where a double tap goes. Deliberately short of the ceiling: a double tap means "let me read
// this", not "take me as far in as this picture goes", and the tap that comes back has to be the
// obvious next thing to do rather than a long way home.
var ZOOM_DOUBLE_TAP = 2.5;
// Below this a scale is the fit for every purpose the reader has. Comparing against 1 exactly
// would leave a picture 1.0000001 across "zoomed", which means pannable, which means the tap that
// closes the preview is being read as a drag.
var ZOOM_EPSILON = 0.0001;

function usableLength(value) {
    return typeof value === "number" && isFinite(value) && value > 0;
}

/**
 * The lightbox's zoom arithmetic, with no node in it.
 *
 * Everything below is in client coordinates, which is what a touch and a wheel both arrive in, and
 * the offset it produces is a CSS `translate` in the same units. Two facts hold the whole thing
 * together and both come from the stylesheet rather than from here: the picture is centred in its
 * frame, so its untransformed middle is the frame's middle; and its `transform-origin` is that
 * middle, so a scale grows it about the same point every anchor below is measured from. A point
 * `u` away from that middle is therefore drawn at `middle + offset + scale * u`, and every function
 * here is that one line rearranged.
 *
 * It is separate from the handlers because its mistakes are the ones a screenshot cannot show. A
 * picture that drifts under the fingers still looks like a picture; a bound computed from an
 * unmeasured frame still looks like a picture, right up until it is dragged off the screen.
 */
export function createImageZoom() {
    var scale = ZOOM_FIT;
    var x = 0;
    var y = 0;
    var ceiling = ZOOM_CEILING_MIN;
    var frame = null;    // the window the picture may move inside, in client coordinates
    var content = null;  // the picture's fitted size, which is its size at scale 1

    function zoomed() { return scale > ZOOM_FIT + ZOOM_EPSILON; }

    /**
     * Pull the offset back inside what the picture is allowed to hide.
     *
     * The rule is that no edge of the picture ever comes inside the frame while the picture is
     * larger than it: the reader can look at any part of it and cannot arrive at a band of
     * background with the picture off the side. That leaves `(scale * size - frame) / 2` of travel
     * each way, and none at all in the direction the picture is still smaller than the frame,
     * which is what keeps a portrait picture centred horizontally however far it is dragged.
     */
    function settle() {
        if (!frame || !content) { x = 0; y = 0; return; }
        var acrossX = Math.max(0, (scale * content.width - frame.width) / 2);
        var acrossY = Math.max(0, (scale * content.height - frame.height) / 2);
        x = Math.min(acrossX, Math.max(-acrossX, x));
        y = Math.min(acrossY, Math.max(-acrossY, y));
    }

    /** Record the geometry a gesture is about to be measured against. */
    function layout(geometry) {
        var g = geometry || {};
        var f = g.frame || {};
        var c = g.content || {};
        var n = g.natural || {};
        frame = usableLength(f.width) && usableLength(f.height)
            ? {
                left: Number(f.left) || 0, top: Number(f.top) || 0,
                width: f.width, height: f.height
            }
            : null;
        content = usableLength(c.width) && usableLength(c.height)
            ? { width: c.width, height: c.height }
            : null;
        var perfect = content && usableLength(n.width) ? n.width / content.width : 0;
        ceiling = Math.min(ZOOM_CEILING_MAX, Math.max(ZOOM_CEILING_MIN, perfect));
        scale = Math.min(ceiling, Math.max(ZOOM_FIT, scale));
        settle();
    }

    function middle() {
        return frame
            ? { x: frame.left + frame.width / 2, y: frame.top + frame.height / 2 }
            : null;
    }

    /** Zoom to an absolute scale, keeping whatever is under the anchor under the anchor. */
    function scaleTo(next, anchorX, anchorY) {
        var target = Math.min(ceiling,
            Math.max(ZOOM_FIT, usableLength(next) ? next : ZOOM_FIT));
        var centre = middle();
        var ratio = target / scale;
        if (centre && ratio !== 1 &&
            typeof anchorX === "number" && typeof anchorY === "number") {
            // The anchor sits at `centre + offset + scale * u` before and after, and `u` is the
            // same point of the picture both times. Eliminating `u` between the two leaves an
            // offset that needs neither the picture's size nor which part of it is on screen.
            x = (anchorX - centre.x) * (1 - ratio) + x * ratio;
            y = (anchorY - centre.y) * (1 - ratio) + y * ratio;
        }
        // Arriving back at the fit recentres, whatever the way up left behind. `settle` would do
        // it wherever the picture is genuinely no larger than its frame, but this is the promise
        // rather than a consequence of one: zooming out must never end holding a corner.
        if (target <= ZOOM_FIT) { x = 0; y = 0; }
        var moved = target !== scale;
        scale = target;
        settle();
        return moved;
    }

    /** The same, relative — which is how a wheel notch and a pinch both arrive. */
    function scaleBy(factor, anchorX, anchorY) {
        if (!usableLength(factor)) return false;
        return scaleTo(scale * factor, anchorX, anchorY);
    }

    /**
     * Drag the picture, and answer whether it actually moved.
     *
     * A picture that fits the screen does not move at all: there is nothing to bring into view and
     * sliding it would only take it out from under the reader. That refusal is doing a second job
     * as well — the answer is what tells a drag from a press, and a press on the backdrop is how
     * the preview is closed.
     */
    function panBy(dx, dy) {
        if (!zoomed() || !frame || !content) return false;
        var wasX = x;
        var wasY = y;
        x += Number(dx) || 0;
        y += Number(dy) || 0;
        settle();
        return x !== wasX || y !== wasY;
    }

    /** What a double tap does: in to a reading distance, or all the way back out. */
    function toggle(anchorX, anchorY) {
        if (zoomed()) return scaleTo(ZOOM_FIT, anchorX, anchorY);
        return scaleTo(Math.min(ceiling, ZOOM_DOUBLE_TAP), anchorX, anchorY);
    }

    /** Forget everything, including the geometry: the next picture is a different size. */
    function reset() {
        scale = ZOOM_FIT;
        x = 0;
        y = 0;
        ceiling = ZOOM_CEILING_MIN;
        frame = null;
        content = null;
    }

    return {
        layout: layout,
        state: function () { return { scale: scale, x: x, y: y }; },
        limits: function () { return { min: ZOOM_FIT, max: ceiling }; },
        zoomed: zoomed,
        scaleTo: scaleTo,
        scaleBy: scaleBy,
        panBy: panBy,
        toggle: toggle,
        reset: reset
    };
}

// A finger may wander this far and still have been a tap, two taps may be this far apart in space
// and this far apart in time and still have been one double tap. The distances are generous
// because a thumb on a phone is: 12px of travel is a steady hand, and 40px between two taps is the
// same intention twice rather than two different places.
var TAP_SLOP = 12;
var DOUBLE_TAP_SLOP = 40;
var TAP_MILLISECONDS = 320;
// A wheel that reports in lines rather than pixels — Firefox — read as pixels zooms by a factor of
// sixteen per notch, which is the whole range of the gesture in one turn.
var WHEEL_LINE_PIXELS = 16;
// Two rates, because a wheel and a trackpad pinch arrive through the same event with an order of
// magnitude between their deltas: a mouse notch is 100 or so, a pinch step is single digits with
// `ctrlKey` set. One rate for both makes whichever it was not tuned for useless.
var WHEEL_ZOOM_RATE = 0.0025;
var PINCH_ZOOM_RATE = 0.012;

function stopGesture(event) {
    if (event && event.cancelable !== false && typeof event.preventDefault === "function") {
        event.preventDefault();
    }
}

function touchPoints(list) {
    var points = [];
    for (var i = 0; list && i < list.length; i += 1) {
        points.push({ x: list[i].clientX, y: list[i].clientY });
    }
    return points;
}

function apart(a, b) { return Math.hypot(b.x - a.x, b.y - a.y); }
function between(a, b) { return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }; }

/**
 * Accessible modal behavior kept independent of layout so it can be exercised without a browser.
 *
 * It also owns the zoom, because a picture the reader cannot enlarge is a picture they cannot
 * read: `index.html` turns the browser's own pinch off for the whole page, and says why, so the
 * gesture has to be answered here or nowhere. `createImageZoom` above holds the arithmetic; this
 * function is only the translation from four kinds of input into it — two fingers, one finger
 * twice, one finger dragged, and a wheel — plus the one existing behavior that translation can
 * break, which is the press on the backdrop that closes the preview.
 *
 * Every write to a presentational property below asks whether the node has one. That is not
 * defensiveness about browsers; it is what the first sentence means in practice. The suite drives
 * this with stand-in nodes carrying only what the behavior under test needs, and a stand-in with
 * no `style` should be a lightbox that cannot be zoomed rather than a TypeError three assertions
 * further down.
 */
export function createImageLightbox(dialog, image, closeButton, doc = document, options = {}) {
    var previous = null;
    var onImageError = null;
    var zoom = createImageZoom();
    var clock = typeof options.now === "function"
        ? options.now
        : function () { return Date.now(); };
    // The window the picture moves inside. The stylesheet centres the picture in this element
    // rather than in the dialog, which is what leaves the padding around it a place to press to
    // close — so it is also the rectangle the pan bounds are measured against. A stand-in dialog
    // with no frame inside it falls back to itself and simply never measures.
    var frame = (dialog.querySelector && dialog.querySelector(".image-lightbox-frame")) || dialog;
    var measured = null;
    var pinch = null;   // two fingers: where their middle was and how far apart they were
    var finger = null;  // one finger: where it last was
    var drag = null;    // one held pointer button: where it last was
    var tap = null;     // a tap in progress: where it started and when
    var lastTap = null; // the last completed tap, waiting to be half of a double one
    var press = null;   // where the press now underway began, whatever it has become since
    var moved = false;  // has that press travelled far enough to stop being a press?

    /**
     * Whether the press underway is still a press.
     *
     * The question is travel, not effect. Asking whether the picture moved is the same question
     * almost everywhere and wrong in the one place a browser found it: a 16:9 picture a little
     * over the fit in a 4:3 frame has no vertical travel at all, so a drag straight up moves
     * nothing, and the release on the backdrop reads as an ordinary press and closes the preview
     * mid-gesture. `tap` below asks something adjacent and is not this — it carries a clock and a
     * target because a double tap has to know when and on what, and it does not exist at all while
     * two fingers are down.
     */
    function pressBegan(x, y) {
        press = { x: x, y: y };
        moved = false;
    }

    function pressReached(x, y) {
        if (press && apart({ x: x, y: y }, press) > TAP_SLOP) moved = true;
    }

    function draw() {
        var view = zoom.state();
        if (image.style) {
            image.style.transform =
                "translate(" + view.x + "px, " + view.y + "px) scale(" + view.scale + ")";
        }
        if (image.dataset) image.dataset.zoomed = zoom.zoomed() ? "true" : "false";
    }

    /**
     * Whether the next change of scale is worth animating.
     *
     * Only the double tap is: it is a jump between two scales and the reader did not choose the
     * path between them. A pinch and a wheel are followed continuously, and a transition on those
     * is a picture arriving where the fingers were 180ms ago.
     */
    function animate(wanted) {
        if (image.dataset) image.dataset.zoomAnimate = wanted ? "true" : "false";
    }

    /**
     * Measure, at the start of a gesture rather than on a resize.
     *
     * `getBoundingClientRect` is a layout read and this is the one moment it is certainly free:
     * the reader has just put a finger down and nothing has been written yet. Doing it here rather
     * than from a resize listener also means the module needs no window at all, and an orientation
     * change is picked up by the first gesture after it rather than never.
     */
    function relayout() {
        var box = frame && frame.getBoundingClientRect ? frame.getBoundingClientRect() : null;
        measured = box ? {
            // `offsetWidth` is the laid-out size and ignores the transform, which is exactly what
            // is wanted: the picture's size at scale 1 stays knowable while it is enlarged.
            frame: { left: box.left, top: box.top, width: box.width, height: box.height },
            content: { width: image.offsetWidth, height: image.offsetHeight },
            natural: { width: image.naturalWidth, height: image.naturalHeight }
        } : null;
        zoom.layout(measured);
    }

    function close() {
        if (dialog.hidden) return;
        dialog.hidden = true;
        image.removeAttribute("src");
        zoom.reset();
        measured = null;
        pinch = null;
        finger = null;
        drag = null;
        tap = null;
        lastTap = null;
        press = null;
        moved = false;
        animate(false);
        draw();
        var restore = previous;
        previous = null;
        onImageError = null;
        if (restore && restore.focus) restore.focus({ preventScroll: true });
    }

    function open(trigger, src, failed) {
        previous = trigger;
        onImageError = failed || null;
        // Before the source, not after: the next picture is a different size and a different
        // number of pixels, and the reader must not be handed the last one's magnification for
        // however long it takes the new bytes to arrive.
        zoom.reset();
        measured = null;
        animate(false);
        draw();
        image.src = src;
        dialog.hidden = false;
        closeButton.focus();
    }

    /* ---- two fingers, one finger, and a finger twice ---------------------- */

    function onTouchStart(event) {
        if (dialog.hidden) return;
        animate(false);
        relayout();
        var touches = touchPoints(event.touches);
        if (touches.length >= 2) {
            // Two fingers are never a press, whatever they go on to do to the picture.
            press = null;
            moved = true;
            pinch = {
                span: apart(touches[0], touches[1]),
                scale: zoom.state().scale,
                middle: between(touches[0], touches[1])
            };
            finger = null;
            tap = null;
            stopGesture(event);
            return;
        }
        pinch = null;
        finger = touches.length === 1 ? { x: touches[0].x, y: touches[0].y } : null;
        tap = finger ? { x: finger.x, y: finger.y, at: clock(), on: event.target } : null;
        if (finger) pressBegan(finger.x, finger.y);
    }

    function onTouchMove(event) {
        if (dialog.hidden) return;
        var touches = touchPoints(event.touches);
        if (pinch && touches.length >= 2) {
            var middle = between(touches[0], touches[1]);
            var span = apart(touches[0], touches[1]);
            // Anchored on where the middle *was*, not where it is: this step's scale keeps the
            // point the fingers are holding still, and the pan that follows is what carries it to
            // where they have moved to. Anchoring on the new middle instead makes the two
            // corrections cancel, and a pinch dragged across the screen zooms without moving.
            var changed = pinch.span > 0
                && zoom.scaleTo(pinch.scale * (span / pinch.span), pinch.middle.x, pinch.middle.y);
            if (zoom.panBy(middle.x - pinch.middle.x, middle.y - pinch.middle.y)) changed = true;
            pinch.middle = middle;
            tap = null;
            if (changed) draw();
            stopGesture(event);
            return;
        }
        if (!finger || touches.length !== 1) return;
        var dx = touches[0].x - finger.x;
        var dy = touches[0].y - finger.y;
        pressReached(touches[0].x, touches[0].y);
        if (tap && apart(touches[0], tap) > TAP_SLOP) tap = null;
        finger.x = touches[0].x;
        finger.y = touches[0].y;
        // A fitted picture has nowhere to go, and taking the event anyway would be taking it from
        // whatever the page would rather do with a finger that is only resting on the backdrop.
        if (!zoom.zoomed()) return;
        if (zoom.panBy(dx, dy)) draw();
        stopGesture(event);
    }

    function onTouchEnd(event) {
        if (dialog.hidden) return;
        var remaining = touchPoints(event.touches);
        if (remaining.length >= 2) return;
        if (remaining.length === 1) {
            // A pinch that has lost a finger becomes a drag, and the drag has to start from where
            // the finger that stayed is — otherwise the picture jumps by the gap between them.
            pinch = null;
            finger = { x: remaining[0].x, y: remaining[0].y };
            tap = null;
            return;
        }
        pinch = null;
        finger = null;
        press = null;
        var ended = touchPoints(event.changedTouches)[0] || null;
        var candidate = tap;
        tap = null;
        if (!ended || !candidate || clock() - candidate.at > TAP_MILLISECONDS ||
            apart(ended, candidate) > TAP_SLOP || candidate.on === closeButton) {
            lastTap = null;
            return;
        }
        if (lastTap && clock() - lastTap.at <= TAP_MILLISECONDS &&
            apart(ended, lastTap) <= DOUBLE_TAP_SLOP) {
            lastTap = null;
            moved = true;
            relayout();
            animate(true);
            zoom.toggle(ended.x, ended.y);
            draw();
            // The second tap is the gesture and not a press. Without this the browser follows it
            // with a synthesised click, and a synthesised click on the backdrop closes the preview
            // the reader has just enlarged.
            stopGesture(event);
            return;
        }
        lastTap = { x: ended.x, y: ended.y, at: clock() };
    }

    function onTouchCancel() {
        pinch = null;
        finger = null;
        press = null;
        tap = null;
        lastTap = null;
    }

    /* ---- a wheel, a trackpad, and a held mouse button --------------------- */

    function onWheel(event) {
        if (dialog.hidden) return;
        // Unconditionally, before anything is decided: a wheel that reaches the page scrolls the
        // transcript behind the preview, and the reader comes back to a different conversation.
        stopGesture(event);
        animate(false);
        relayout();
        var step = event.deltaMode === 1 ? WHEEL_LINE_PIXELS
            : (event.deltaMode === 2 ? ((measured && measured.frame.height) || 1) : 1);
        var rate = event.ctrlKey ? PINCH_ZOOM_RATE : WHEEL_ZOOM_RATE;
        // Exponential rather than linear, so that a notch in and a notch out are the same size of
        // gesture wherever the reader already is, and neither can walk the scale to zero.
        var factor = Math.exp(-(Number(event.deltaY) || 0) * step * rate);
        if (zoom.scaleBy(factor, event.clientX, event.clientY)) draw();
    }

    function onPointerDown(event) {
        // A finger arrives here as well as through the touch handlers, which can do more with it.
        // Answering both drags the picture twice as far as the finger went.
        if (dialog.hidden || event.pointerType === "touch") return;
        // Recorded before the picture is asked whether it can move: a sweep across a fitted
        // picture starts no drag at all and is still not a press on what is behind it.
        pressBegan(event.clientX, event.clientY);
        animate(false);
        relayout();
        if (!zoom.zoomed()) return;
        drag = { x: event.clientX, y: event.clientY };
        if (image.dataset) image.dataset.panning = "true";
    }

    function onPointerMove(event) {
        if (dialog.hidden) return;
        pressReached(event.clientX, event.clientY);
        if (!drag) return;
        var dx = event.clientX - drag.x;
        var dy = event.clientY - drag.y;
        drag.x = event.clientX;
        drag.y = event.clientY;
        if (zoom.panBy(dx, dy)) draw();
    }

    function onPointerUp() {
        // The press is over here rather than at the click, so that a pointer wandering across the
        // page between two presses cannot arm the guard against the second one.
        press = null;
        if (!drag) return;
        drag = null;
        if (image.dataset) image.dataset.panning = "false";
    }

    function onDoubleClick(event) {
        if (dialog.hidden) return;
        stopGesture(event);
        moved = true;
        animate(true);
        relayout();
        zoom.toggle(event.clientX, event.clientY);
        draw();
    }

    closeButton.addEventListener("click", close);
    dialog.addEventListener("click", function (event) {
        // A press that travelled is not a press on the backdrop, whatever the browser decides the
        // common ancestor of where it started and where it ended was. Dragging an enlarged picture
        // out onto the padding around it produces exactly that — a click whose target is the
        // dialog, which is the test this handler was already making — and the preview closes
        // mid-drag. `moved` is cleared at the start of every press, so it can only ever swallow
        // the click of the gesture that set it.
        if (moved) { moved = false; return; }
        if (event.target === dialog) close();
    });
    // Not passive: all three of these have to be able to refuse the browser's own handling of the
    // gesture, and a listener that says it will not is never asked.
    dialog.addEventListener("wheel", onWheel, { passive: false });
    dialog.addEventListener("touchstart", onTouchStart, { passive: false });
    dialog.addEventListener("touchmove", onTouchMove, { passive: false });
    dialog.addEventListener("touchend", onTouchEnd, { passive: false });
    dialog.addEventListener("touchcancel", onTouchCancel);
    dialog.addEventListener("pointerdown", onPointerDown);
    // On the document rather than the dialog: a drag that leaves the picture is still that drag,
    // and a button released over the browser's own chrome still ends it.
    doc.addEventListener("pointermove", onPointerMove);
    doc.addEventListener("pointerup", onPointerUp);
    doc.addEventListener("pointercancel", onPointerUp);
    image.addEventListener("dblclick", onDoubleClick);
    // The size of the picture is not known until the bytes are, and every bound below depends on
    // it. This is also the only measurement that happens without a gesture to hang it on.
    image.addEventListener("load", function () {
        relayout();
        draw();
    });
    image.addEventListener("error", function () {
        if (onImageError) onImageError();
        close();
    });
    doc.addEventListener("keydown", function (event) {
        if (dialog.hidden || event.key !== "Escape") return;
        event.preventDefault();
        event.stopPropagation();
        close();
    }, true);
    return { open: open, close: close };
}
