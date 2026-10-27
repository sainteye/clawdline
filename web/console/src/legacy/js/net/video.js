import { base64Bytes } from "./cloud-crypto.js";
import { normalizeDocumentLocator, isVideoPath, VIDEO_MAX_BYTES, VIDEO_CHUNK_BYTES } from "./document-links.js";

function invalid() {
    return Object.assign(new TypeError("The video chunk did not match the requested file."), { code: "video_answer_invalid" });
}

/** Validate identity, range and encoded size before allocating decoded bytes. */
export function videoChunkAnswer(wanted, offset, revision, body) {
    const locator = normalizeDocumentLocator(wanted);
    const keys = ["scope", "path", "media_type", "total_bytes", "offset", "revision", "byte_count", "data"];
    if (locator.scope === "task") keys.push("task");
    if (!isVideoPath(locator.path) || !body || typeof body !== "object" || Array.isArray(body) ||
        Object.keys(body).length !== keys.length || Object.keys(body).some((key) => !keys.includes(key)) ||
        body.scope !== locator.scope || body.path !== locator.path ||
        (locator.scope === "task" && body.task !== locator.task) || body.media_type !== "video/mp4" ||
        !Number.isSafeInteger(offset) || offset < 0 || offset % VIDEO_CHUNK_BYTES !== 0 ||
        body.offset !== offset || !Number.isSafeInteger(body.total_bytes) || body.total_bytes <= offset || body.total_bytes > VIDEO_MAX_BYTES ||
        !Number.isSafeInteger(body.byte_count) || body.byte_count !== Math.min(VIDEO_CHUNK_BYTES, body.total_bytes - offset) ||
        typeof body.revision !== "string" || !body.revision || body.revision.length > 128 || /[\u0000-\u001f\u007f]/.test(body.revision) ||
        (revision && body.revision !== revision) || (offset > 0 && !revision) || typeof body.data !== "string" ||
        body.data.length !== 4 * Math.ceil(body.byte_count / 3)) throw invalid();
    let bytes;
    try { bytes = base64Bytes(body.data, "video data"); } catch { throw invalid(); }
    if (bytes.length !== body.byte_count) throw invalid();
    return { bytes, revision: body.revision, total: body.total_bytes };
}

function checkAbort(signal) {
    if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
}

/** One sequential transfer, no speculative fetches or automatic retries. */
export async function loadVideo(wanted, readChunk, options = {}) {
    const locator = normalizeDocumentLocator(wanted);
    if (!isVideoPath(locator.path)) throw invalid();
    const chunks = [];
    let offset = 0, revision = "", total = null;
    try {
        do {
            checkAbort(options.signal);
            const body = await readChunk(offset, revision);
            checkAbort(options.signal);
            const chunk = videoChunkAnswer(locator, offset, revision, body);
            if (total !== null && total !== chunk.total) throw invalid();
            revision = chunk.revision;
            total = chunk.total;
            chunks.push(chunk.bytes);
            offset += chunk.bytes.length;
            options.progress?.(Math.floor(offset * 100 / total));
        } while (offset < total);
        return { ...locator, media_type: "video/mp4", byte_count: total, blob: new Blob(chunks, { type: "video/mp4" }) };
    } finally {
        chunks.length = 0;
    }
}

/** A trusted player in an opaque-origin frame; media cannot reach the app DOM. */
export function mountVideo(element, answer, errorText) {
    const doc = element.ownerDocument;
    const win = doc.defaultView;
    const frame = doc.createElement("iframe");
    const error = doc.createElement("p");
    error.className = "document-video-error";
    error.setAttribute("role", "alert");
    error.hidden = true;
    frame.className = "document-video";
    frame.title = answer.path;
    frame.setAttribute("sandbox", "allow-scripts");
    frame.setAttribute("referrerpolicy", "no-referrer");
    frame.allowFullscreen = true;
    let closed = false;
    const nonce = win.crypto.randomUUID();
    const token = win.crypto.randomUUID();
    // The only script is this trusted, nonce-authorized native-event adapter.
    // No filename, author HTML, external URL or media bytes enter its markup.
    const script = 'const video=document.querySelector("video");' +
        'const send=(kind,extra={})=>parent.postMessage({token:' + JSON.stringify(token) + ',kind,...extra},"*");' +
        'video.addEventListener("error",()=>send("error"));' +
        'video.addEventListener("loadedmetadata",()=>send("metadata",{width:video.videoWidth,height:video.videoHeight}));' +
        'let url=null;const clear=()=>{video.pause();video.removeAttribute("src");video.load();if(url)URL.revokeObjectURL(url);url=null};' +
        'addEventListener("pagehide",clear);' +
        'addEventListener("message",event=>{if(event.source!==parent||event.data?.token!==' + JSON.stringify(token) + ')return;' +
        'if(event.data.kind==="dispose"){clear();return}if(event.data.kind!=="load"||!(event.data.blob instanceof Blob))return;' +
        'clear();url=URL.createObjectURL(event.data.blob);video.src=url});';
    const receive = (event) => {
        if (event.source !== frame.contentWindow || event.data?.token !== token) return;
        if (event.data.kind === "error") { error.textContent = errorText; error.hidden = false; }
        if (event.data.kind === "metadata" && Number.isSafeInteger(event.data.width) && event.data.width > 0 &&
            Number.isSafeInteger(event.data.height) && event.data.height > 0)
            frame.style.aspectRatio = event.data.width + " / " + event.data.height;
    };
    win.addEventListener("message", receive);
    frame.addEventListener("load", () => {
        if (!closed) frame.contentWindow.postMessage({ token, kind: "load", blob: answer.blob }, "*");
    }, { once: true });
    frame.srcdoc = '<!doctype html><html><head><meta http-equiv="Content-Security-Policy" ' +
        'content="default-src \'none\'; media-src blob:; style-src \'unsafe-inline\'; script-src \'nonce-' + nonce + '\'; connect-src \'none\'; base-uri \'none\'; form-action \'none\'">' +
        '<meta name="viewport" content="width=device-width,initial-scale=1">' +
        '<style>html,body{margin:0;width:100%;height:100%;overflow:hidden;background:#000}video{display:block;width:100%;height:100%;object-fit:contain}</style>' +
        '</head><body><video controls playsinline preload="metadata"></video>' +
        '<script nonce="' + nonce + '">' + script + '</script></body></html>';
    element.replaceChildren(frame, error);
    return () => {
        closed = true;
        win.removeEventListener("message", receive);
        frame.contentWindow?.postMessage({ token, kind: "dispose" }, "*");
        // Removing the browsing context stops its native media decoder.
        element.replaceChildren();
    };
}
