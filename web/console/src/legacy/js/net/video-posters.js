import { openVideoStream } from "./video-stream.js";
import { videoChunkAnswer } from "./video.js";
import { VIDEO_CHUNK_BYTES, normalizeDocumentLocator } from "./document-links.js";

export const VIDEO_PREVIEW_FRAMES = 4;
const fractions = [0.35, 0.60, 0.12, 0.82];
const unavailable = () => Object.assign(new Error("Video preview read budget exhausted"), { code: "video_preview_unavailable" });

/** One revision-pinned budget shared by all candidate frames, including metadata. */
export async function openVideoPosters(value, readChunk, options) {
    const locator = normalizeDocumentLocator(value), held = new Map();
    let total, revision = "", received = 0, pending, started = false;
    const read = async (offset, requested) => {
        if (options.signal.aborted) throw new DOMException("Aborted", "AbortError");
        if (held.has(offset)) return held.get(offset);
        // A previous decoder can still be finishing a read as its frame is
        // captured. Share that result before another candidate reads source.
        if (pending) { await pending; return read(offset, requested); }
        const count = total === undefined ? VIDEO_CHUNK_BYTES : Math.min(VIDEO_CHUNK_BYTES, total - offset);
        if (received + count > options.byteBudget) throw unavailable();
        pending = (async () => {
            const raw = await readChunk(offset, requested || revision);
            const answer = videoChunkAnswer(locator, offset, requested || revision, raw);
            if (total !== undefined && answer.total !== total) throw unavailable();
            total = answer.total; revision = answer.revision; received += answer.bytes.length;
            held.set(offset, raw);
            return raw;
        })();
        try { return await pending; } finally { pending = undefined; }
    };
    // The first response establishes identity and revision before decoding.
    await read(0, "");
    return { ...locator, media_type: "video/mp4", async frames(capture) {
        if (started) throw unavailable();
        started = true;
        let completed = 0;
        try {
            for (const fraction of fractions.slice(0, VIDEO_PREVIEW_FRAMES)) {
                if (options.signal.aborted) throw new DOMException("Aborted", "AbortError");
                const controller = new AbortController(), cancel = () => controller.abort();
                options.signal.addEventListener("abort", cancel, { once: true });
                try {
                    const answer = await openVideoStream(locator, read, { signal: controller.signal,
                        videoOnly: true, startFraction: fraction });
                    await capture(answer, controller.signal);
                    completed++;
                } catch (error) {
                    if (options.signal.aborted) throw error;
                    if (error.code === "video_preview_unavailable" && completed) break;
                    if (error.code !== "video_format_unsupported") throw error;
                } finally {
                    controller.abort(); options.signal.removeEventListener("abort", cancel);
                }
            }
            if (!completed) throw unavailable();
        } finally { held.clear(); }
    } };
}
