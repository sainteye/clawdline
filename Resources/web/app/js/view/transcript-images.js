/* Expiring image artifacts in a transcript. Data decides state; markup never comes from it. */

const artifactID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

/** The initial client state for one closed artifact reference. */
export function artifactPresentation(artifact, now = Math.floor(Date.now() / 1000)) {
    var a = artifact || {};
    var valid = typeof a.id === "string" && artifactID.test(a.id) &&
        a.media_type === "image/png" && Number.isSafeInteger(a.byte_count) && a.byte_count > 0 &&
        Number.isSafeInteger(a.width) && a.width > 0 &&
        Number.isSafeInteger(a.height) && a.height > 0 &&
        Number.isSafeInteger(a.expires_at) && a.expires_at > 0;
    if (!valid || a.expires_at <= now) return { state: "expired" };
    return {
        state: "loading",
        // Relative and same-origin on purpose: localhost today and clawdline.com later have the
        // same contract, and neither transcript nor artifact reference persists a public URL.
        url: "/v1/artifacts/images/" + encodeURIComponent(a.id),
        width: a.width,
        height: a.height
    };
}

/** Connect a static, locally-authored tile to artifact data using DOM properties only. */
export function connectArtifactTile(tile, artifact, options = {}) {
    var image = tile.querySelector(".message-image");
    var status = tile.querySelector(".message-image-state");
    var presentation = artifactPresentation(artifact, options.now);

    function expire() {
        tile.dataset.imageState = "expired";
        tile.disabled = true;
        image.hidden = true;
        image.removeAttribute("src");
        status.hidden = false;
        status.textContent = options.expiredLabel || "Image expired";
    }

    if (presentation.state === "expired") {
        expire();
        return { presentation: presentation, expire: expire };
    }

    tile.dataset.imageState = "loading";
    tile.disabled = true;
    image.hidden = true;
    status.hidden = false;
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
    image.src = presentation.url;
    return { presentation: presentation, expire: expire };
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
