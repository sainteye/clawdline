import { createFile } from "mp4box";
import { VIDEO_CHUNK_BYTES, VIDEO_MAX_BYTES, normalizeDocumentLocator, isVideoPath } from "./document-links.js";
import { videoChunkAnswer } from "./video.js";

function failure() {
    return Object.assign(new Error("The video could not be prepared for streaming."), { code: "video_format_unsupported" });
}
function active(signal) {
    if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
}

/** The same authorized chunks, remuxed locally; no media server or public URL. */
export async function openVideoStream(wanted, readChunk, options = {}) {
    const locator = normalizeDocumentLocator(wanted);
    if (!isVideoPath(locator.path)) throw failure();
    active(options.signal);
    const first = videoChunkAnswer(locator, 0, "", await readChunk(0, ""));
    active(options.signal);
    options.progress?.(Math.floor(first.bytes.length * 100 / first.total));
    let started = false;
    return { ...locator, media_type: "video/mp4", byte_count: first.total,
        async stream(sink) {
            if (started) throw failure();
            started = true;
            const file = createFile(false);
            // Metadata discovery may jump over mdat to a tail moov. Reuse those
            // authenticated chunks when feeding media, rather than downloading twice.
            const held = new Map([[0, first.bytes]]);
            const seen = new Set([0]);
            let received = first.bytes.length;
            let info, parseError, batch = [], batchBytes = 0;
            const get = async (offset) => {
                active(options.signal);
                if (held.has(offset)) return held.get(offset);
                const count = Math.min(VIDEO_CHUNK_BYTES, first.total - offset);
                if (received + count > (options.byteBudget ?? VIDEO_MAX_BYTES))
                    throw Object.assign(new Error("Video preview read budget exhausted"), { code: "video_preview_unavailable" });
                const answer = videoChunkAnswer(locator, offset, first.revision, await readChunk(offset, first.revision));
                active(options.signal);
                if (answer.total !== first.total) throw failure();
                received += answer.bytes.length;
                seen.add(offset);
                const loaded = [...seen].reduce((n, at) => n + Math.min(VIDEO_CHUNK_BYTES, first.total - at), 0);
                options.progress?.(Math.floor(loaded * 100 / first.total));
                return answer.bytes;
            };
            const append = (bytes, offset) => {
                const buffer = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
                buffer.fileStart = offset;
                const next = file.appendBuffer(buffer);
                if (parseError) throw failure();
                return next;
            };
            file.onError = () => { parseError = true; };
            file.onReady = (movie) => { info = movie; };
            try {
                let offset = 0;
                while (!info) {
                    const bytes = await get(offset);
                    held.set(offset, bytes);
                    const next = append(bytes, offset);
                    if (info) break;
                    const at = Math.max(offset + VIDEO_CHUNK_BYTES, Math.floor(next / VIDEO_CHUNK_BYTES) * VIDEO_CHUNK_BYTES);
                    if (!Number.isSafeInteger(at) || at <= offset || at >= first.total) throw failure();
                    offset = at;
                }
                const tracks = info.tracks.filter(track => track.type === "video" || (!options.videoOnly && track.type === "audio"));
                const duration = info.duration / info.timescale;
                if (!tracks.some(track => track.type === "video") || !Number.isFinite(duration) || duration <= 0) throw failure();
                // Reject impossible sample extents before the remuxer allocates data.
                let mediaBytes = 0;
                for (const track of tracks) for (const sample of file.getTrackById(track.id).samples) {
                    if (!Number.isSafeInteger(sample.size) || sample.size < 0 || !Number.isSafeInteger(sample.offset) ||
                        sample.offset < 0 || sample.offset + sample.size > first.total) throw failure();
                    mediaBytes += sample.size;
                    if (mediaBytes > first.total) throw failure();
                }
                file.onReady = undefined;
                file.discardMdatData = false;
                file.onSegment = (id, _user, buffer, sample) => {
                    batchBytes += buffer.byteLength;
                    if (batchBytes > VIDEO_MAX_BYTES) throw failure();
                    batch.push({ id, buffer });
                    file.releaseUsedSamples(id, sample);
                };
                // Emit complete samples immediately; the decoder retains keyframe order.
                for (const track of tracks) file.setSegmentOptions(track.id, null, { nbSamples: 1, rapAlignement: false });
                const initial = file.initializeSegmentation("per-track");
                await sink.initialize(initial.map(part => ({ id: part.id, buffer: part.buffer,
                    mime: `${tracks.find(track => track.id === part.id).type}/mp4; codecs="${tracks.find(track => track.id === part.id).codec}"` })), duration, duration * (options.startFraction ?? 0));
                active(options.signal);
                const start = file.seek(duration * (options.startFraction ?? 0), true);
                if (!Number.isSafeInteger(start.offset) || start.offset < 0 || start.offset >= first.total) throw failure();
                file.start();
                const drain = async () => {
                    if (!batch.length) return;
                    const groups = new Map();
                    for (const part of batch) {
                        if (!groups.has(part.id)) groups.set(part.id, []);
                        groups.get(part.id).push(new Uint8Array(part.buffer));
                    }
                    const parts = [...groups].map(([id, chunks]) => {
                        const joined = new Uint8Array(chunks.reduce((size, chunk) => size + chunk.byteLength, 0));
                        let at = 0;
                        for (const chunk of chunks) { joined.set(chunk, at); at += chunk.byteLength; }
                        return { id, buffer: joined.buffer };
                    });
                    batch = []; batchBytes = 0;
                    // Await decoder acknowledgement before reading another encrypted chunk.
                    await sink.append(parts);
                    active(options.signal);
                };
                for (offset = Math.floor(start.offset / VIDEO_CHUNK_BYTES) * VIDEO_CHUNK_BYTES; offset < first.total; offset += VIDEO_CHUNK_BYTES) {
                    const bytes = await get(offset);
                    held.delete(offset);
                    append(bytes, offset);
                    await drain();
                }
                file.flush();
                if (parseError) throw failure();
                await drain();
                await sink.finish();
            } finally {
                file.stop();
                file.onSegment = undefined;
                held.clear(); batch = [];
                // No parser, queued segment or decrypted chunk survives this transfer.
            }
        },
    };
}
