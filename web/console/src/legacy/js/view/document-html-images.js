import { DOCUMENT_MAX_BYTES } from "../net/document-links.js";

// Registered with the daemon's identical decoded-image limits. This second
// gate also protects a new console receiving HTML from an older daemon.
const MAXIMUM_IMAGE_SIDE = 8192;
const MAXIMUM_IMAGE_PIXELS = 16 * 1024 * 1024;
export const MAXIMUM_REPORT_IMAGE_PIXELS = 64 * 1024 * 1024;

function dimensionsOK(width, height) {
    return width > 0 && height > 0 && width <= MAXIMUM_IMAGE_SIDE &&
        height <= MAXIMUM_IMAGE_SIDE && width * height <= MAXIMUM_IMAGE_PIXELS;
}

/** Only canonical base64 PNG/JPEG screenshots, never a resource URL. */
export function validInlineImage(value) { return inlineImagePixels(value) > 0; }

export function inlineImagePixels(value) {
    const dimensions = inlineImageDimensions(value);
    return dimensions ? dimensions.width * dimensions.height : 0;
}

/** The intrinsic size of a validated inline PNG or JPEG. */
export function inlineImageDimensions(value) {
    if (typeof value !== "string" || value.length > DOCUMENT_MAX_BYTES) return null;
    const match = /^data:image\/(png|jpeg);base64,([A-Za-z0-9+/]+={0,2})$/.exec(value);
    if (!match || match[0] !== value) return null;
    let binary;
    try {
        binary = atob(match[2]);
        if (btoa(binary) !== match[2]) return null;
    } catch { return null; }
    const bytes = Uint8Array.from(binary, function (c) { return c.charCodeAt(0); });
    if (match[1] === "png") {
        if (bytes.length < 33 || ![137,80,78,71,13,10,26,10,0,0,0,13,73,72,68,82].every(function (v, i) { return bytes[i] === v; })) return null;
        const header = new DataView(bytes.buffer);
        let offset = 8, ended = false;
        while (offset + 12 <= bytes.length) {
            const length = header.getUint32(offset), end = offset + 12 + length;
            if (end > bytes.length) return null;
            const kind = String.fromCharCode(...bytes.subarray(offset + 4, offset + 8));
            if (["acTL", "fcTL", "fdAT"].includes(kind)) return null;
            if (kind === "IEND") { ended = length === 0 && end === bytes.length; break; }
            offset = end;
        }
        if (!ended) return null;
        const width = header.getUint32(16), height = header.getUint32(20);
        return dimensionsOK(width, height) ? {width, height} : null;
    }
    if (bytes.length < 4 || bytes[0] !== 255 || bytes[1] !== 216 || bytes[bytes.length - 2] !== 255 || bytes[bytes.length - 1] !== 217) return null;
    let offset = 2;
    while (offset < bytes.length) {
        if (bytes[offset++] !== 255) return null;
        while (offset < bytes.length && bytes[offset] === 255) offset++;
        if (offset >= bytes.length) return null;
        const marker = bytes[offset++];
        // SOF must precede the scan. Restart markers have no segment length.
        if (marker === 218 || marker === 217 || marker === 0 || marker === 216) return null;
        if (marker === 1 || marker >= 208 && marker <= 215) continue;
        if (offset + 2 > bytes.length) return null;
        const length = bytes[offset] * 256 + bytes[offset + 1];
        if (length < 2 || offset + length > bytes.length) return null;
        if (marker >= 192 && marker <= 207 && marker !== 196 && marker !== 200 && marker !== 204) {
            if (length < 8) return null;
            const width = bytes[offset + 5] * 256 + bytes[offset + 6], height = bytes[offset + 3] * 256 + bytes[offset + 4];
            return dimensionsOK(width, height) ? {width, height} : null;
        }
        offset += length;
    }
    return null;
}
