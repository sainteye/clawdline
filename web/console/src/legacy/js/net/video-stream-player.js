/** Trusted native controls. MP4 bytes are decoder input, never HTML or script. */
export function mountStreamingVideo(element, answer, errorText, events = {}) {
    const doc = element.ownerDocument, win = doc.defaultView;
    const video = doc.createElement("video");
    video.className = "document-video";
    video.setAttribute("aria-label", answer.path);
    video.controls = true; video.playsInline = true; video.preload = "auto";
    video.disableRemotePlayback = true;
    const error = doc.createElement("p");
    error.className = "document-video-error"; error.setAttribute("role", "alert"); error.hidden = true;
    const lifetime = new AbortController();
    let source, url, closed = false, failed = false;
    const buffers = new Map();
    const refusal = () => Object.assign(new Error(errorText), { code: "video_format_unsupported" });
    const fail = caught => {
        if (closed || failed) return;
        failed = true;
        lifetime.abort(); video.pause();
        error.textContent = errorText; error.hidden = false;
        events.error?.(caught);
    };
    const until = (target, name, action) => new Promise((resolve, reject) => {
        if (lifetime.signal.aborted) { reject(new DOMException("Aborted", "AbortError")); return; }
        const clean = () => {
            target.removeEventListener(name, success); target.removeEventListener("error", bad);
            lifetime.signal.removeEventListener("abort", cancel);
        };
        const success = () => { clean(); resolve(); };
        const bad = () => { clean(); reject(refusal()); };
        const cancel = () => { clean(); reject(new DOMException("Aborted", "AbortError")); };
        target.addEventListener(name, success, { once: true });
        target.addEventListener("error", bad, { once: true });
        lifetime.signal.addEventListener("abort", cancel, { once: true });
        try { action(); } catch (caught) { clean(); reject(caught); }
    });
    video.addEventListener("error", () => fail(refusal()), { signal: lifetime.signal });
    video.addEventListener("loadedmetadata", () => {
        if (video.videoWidth && video.videoHeight) video.style.aspectRatio = video.videoWidth + " / " + video.videoHeight;
    }, { signal: lifetime.signal });
    element.replaceChildren(video, error);
    answer.stream({
        async initialize(tracks, duration) {
            const Media = win.ManagedMediaSource || win.MediaSource;
            if (!Media || tracks.some(track => !Media.isTypeSupported(track.mime))) throw refusal();
            source = new Media();
            await until(source, "sourceopen", () => {
                url = win.URL.createObjectURL(source); video.src = url; video.load();
            });
            source.duration = duration;
            // Declare both tracks before either initialization segment reaches the decoder.
            for (const track of tracks) buffers.set(track.id, source.addSourceBuffer(track.mime));
            for (const track of tracks) await until(buffers.get(track.id), "updateend", () => buffers.get(track.id).appendBuffer(track.buffer));
        },
        async append(parts) {
            for (const part of parts) {
                const buffer = buffers.get(part.id);
                await until(buffer, "updateend", () => buffer.appendBuffer(part.buffer));
            }
        },
        async finish() { source.endOfStream(); },
    }).then(() => { if (!closed && !failed) events.ready?.(); }).catch(caught => fail(caught));
    return () => {
        closed = true; lifetime.abort();
        video.pause(); video.removeAttribute("src"); video.load();
        if (url) win.URL.revokeObjectURL(url);
        buffers.clear(); source = null;
        element.replaceChildren();
    };
}
