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

/** Accessible modal behavior kept independent of layout so it can be exercised without a browser. */
export function createImageLightbox(dialog, image, closeButton, doc = document) {
    var previous = null;
    var onImageError = null;

    function close() {
        if (dialog.hidden) return;
        dialog.hidden = true;
        image.removeAttribute("src");
        var restore = previous;
        previous = null;
        onImageError = null;
        if (restore && restore.focus) restore.focus({ preventScroll: true });
    }

    function open(trigger, src, failed) {
        previous = trigger;
        onImageError = failed || null;
        image.src = src;
        dialog.hidden = false;
        closeButton.focus();
    }

    closeButton.addEventListener("click", close);
    dialog.addEventListener("click", function (event) {
        if (event.target === dialog) close();
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
