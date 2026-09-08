import { base64Bytes } from "./cloud-crypto.js";

export const DOCUMENT_MAX_BYTES = 2 * 1024 * 1024;
export const CANONICAL_DOCUMENT_ORIGIN = "https://app.clawdline.com";
const DOCUMENT_MAX_PATH = 512;
const DOCUMENT_MAX_DEPTH = 6;
const DOCUMENT_MAX_LIST = 200;
const DOCUMENT_EXTENSIONS = new Set(["md", "markdown", "txt"]);
const TASK_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

function documentLinkError(code, message) {
    var error = new Error(message);
    error.code = code;
    return error;
}

function textIdentity(value, name) {
    if (typeof value !== "string" || !value || value.length > 128 ||
        /[\u0000-\u001f\u007f-\u009f]/.test(value)) {
        throw new TypeError("document locator has an invalid " + name);
    }
    return value;
}

/** A list request also needs the fleet identity, without inventing a document path yet. */
export function normalizeDocumentIdentity(value) {
    if (!value || typeof value !== "object" || Array.isArray(value) ||
        Object.keys(value).length !== 2 ||
        !Object.prototype.hasOwnProperty.call(value, "machine") ||
        !Object.prototype.hasOwnProperty.call(value, "session")) {
        throw new TypeError("document list needs an explicit machine and session");
    }
    return {
        machine: textIdentity(value.machine, "machine"),
        session: textIdentity(value.session, "session")
    };
}

/** Resolve the shared Session row key without ever choosing one machine from a duplicate id. */
export function documentIdentityForSession(rows, id, transportKind) {
    if (!Array.isArray(rows) || typeof id !== "string" || !id) {
        throw documentLinkError("not_found", "this session is not in the document inventory");
    }
    var identities = [];
    rows.forEach(function (row) {
        if (!row || (row.id !== id && row.session !== id)) return;
        var candidate = row.identity;
        if (!candidate && transportKind !== "cloud") {
            candidate = { machine: "this-mac", session: row.session || row.id };
        }
        if (!candidate) {
            throw documentLinkError("cloud_session_identity_missing",
                "the Cloud session row carries no explicit machine identity");
        }
        var normalized;
        try { normalized = normalizeDocumentIdentity(candidate); }
        catch (error) {
            throw documentLinkError("cloud_session_identity_missing",
                "the Cloud session row carries an invalid machine identity");
        }
        if (!identities.some(function (seen) {
            return seen.machine === normalized.machine && seen.session === normalized.session;
        })) identities.push(normalized);
    });
    if (identities.length === 1) return identities[0];
    if (identities.length > 1) {
        throw documentLinkError("cloud_session_ambiguous",
            "more than one Mac published this session id");
    }
    throw documentLinkError("not_found", "this session is not in the document inventory");
}

function documentPath(value) {
    if (typeof value !== "string" || !value || value.length > DOCUMENT_MAX_PATH ||
        value.charAt(0) === "/" || /[\u0000-\u001f\u007f-\u009f]/.test(value)) {
        throw new TypeError("document locator has an invalid path");
    }
    var parts = value.split("/");
    if (!parts.length || parts.length > DOCUMENT_MAX_DEPTH || parts.some(function (part) {
        return !part || part.charAt(0) === ".";
    })) throw new TypeError("document locator has an invalid path");
    var name = parts[parts.length - 1];
    var dot = name.lastIndexOf(".");
    if (dot <= 0 || !DOCUMENT_EXTENSIONS.has(name.slice(dot + 1).toLowerCase())) {
        throw new TypeError("document locator has an invalid path");
    }
    return value;
}

/** The only identity a direct document address is allowed to carry. */
export function normalizeDocumentLocator(value) {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
        throw new TypeError("document locator must be an object");
    }
    var scope = value.scope;
    if (scope !== "project" && scope !== "task") {
        throw new TypeError("document locator has an invalid scope");
    }
    var allowed = scope === "task"
        ? ["machine", "session", "scope", "task", "path"]
        : ["machine", "session", "scope", "path"];
    var keys = Object.keys(value).sort();
    if (keys.length !== allowed.length || allowed.some(function (key) {
        return !Object.prototype.hasOwnProperty.call(value, key);
    }) || keys.some(function (key) { return allowed.indexOf(key) < 0; })) {
        throw new TypeError("document locator carries fields outside its identity");
    }
    var out = {
        machine: textIdentity(value.machine, "machine"),
        session: textIdentity(value.session, "session"),
        scope: scope
    };
    if (scope === "task") {
        if (typeof value.task !== "string" || !TASK_ID.test(value.task)) {
            throw new TypeError("document locator has an invalid task");
        }
        out.task = value.task;
    }
    out.path = documentPath(value.path);
    return out;
}

function locatorFields(locator) {
    var fields = new URLSearchParams();
    fields.set("document", "1");
    fields.set("machine", locator.machine);
    fields.set("session", locator.session);
    fields.set("scope", locator.scope);
    if (locator.scope === "task") fields.set("task", locator.task);
    fields.set("path", locator.path);
    return fields;
}

export function documentHash(value) {
    return "#" + locatorFields(normalizeDocumentLocator(value)).toString();
}

/** Parse a strict whole fragment. Extra fields and duplicate fields both fail closed. */
export function documentLocatorFromHash(hash) {
    var source = String(hash || "");
    if (source.charAt(0) === "#") source = source.slice(1);
    var fields = new URLSearchParams(source);
    var scope = fields.get("scope");
    var wanted = scope === "task"
        ? ["document", "machine", "session", "scope", "task", "path"]
        : ["document", "machine", "session", "scope", "path"];
    var keys = Array.from(fields.keys());
    if (fields.get("document") !== "1" || keys.length !== wanted.length ||
        wanted.some(function (key) { return fields.getAll(key).length !== 1; }) ||
        keys.some(function (key) { return wanted.indexOf(key) < 0; })) return null;
    var locator = {
        machine: fields.get("machine"), session: fields.get("session"),
        scope: scope, path: fields.get("path")
    };
    if (scope === "task") locator.task = fields.get("task");
    try { return normalizeDocumentLocator(locator); } catch (error) { return null; }
}

export function documentShareURL(origin, value) {
    var locator;
    var url;
    try {
        locator = normalizeDocumentLocator(value);
        url = new URL(origin);
    } catch (error) {
        throw documentLinkError("document_share_unavailable",
            "the canonical Cloud document link could not be constructed");
    }
    if (url.origin !== CANONICAL_DOCUMENT_ORIGIN || locator.machine === "this-mac") {
        throw documentLinkError("document_share_unavailable",
            "only a Cloud document with an explicit Mac identity can be shared across devices");
    }
    url = new URL(CANONICAL_DOCUMENT_ORIGIN + "/");
    url.pathname = "/";
    url.search = "";
    url.hash = documentHash(locator);
    return url.toString();
}

function listingRow(row, identity) {
    if (!row || typeof row !== "object" || Array.isArray(row)) {
        throw new TypeError("document listing metadata is malformed");
    }
    var scope = row.scope;
    var allowed = scope === "task"
        ? ["scope", "task", "path", "bytes", "modified", "title"]
        : ["scope", "path", "bytes", "modified"];
    var keys = Object.keys(row);
    if (keys.length !== allowed.length || keys.some(function (key) { return allowed.indexOf(key) < 0; })) {
        throw new TypeError("document listing metadata carries an unknown field");
    }
    if (!Number.isSafeInteger(row.bytes) || row.bytes < 0 || row.bytes > DOCUMENT_MAX_BYTES ||
        typeof row.modified !== "number" || !Number.isFinite(row.modified)) {
        throw new TypeError("document listing metadata has invalid bounds");
    }
    var locator = { machine: identity.machine, session: identity.session,
        scope: scope, path: row.path };
    if (scope === "task") locator.task = row.task;
    locator = normalizeDocumentLocator(locator);
    locator.bytes = row.bytes;
    locator.modified = row.modified;
    if (scope === "task") {
        if (typeof row.title !== "string" || row.title.length > 300) {
            throw new TypeError("document listing metadata has an invalid title");
        }
        locator.title = row.title;
    }
    return locator;
}

export function documentListing(body, identity) {
    if (!body || typeof body !== "object" || Array.isArray(body) ||
        Object.keys(body).length !== 1 || !Array.isArray(body.documents) ||
        body.documents.length > DOCUMENT_MAX_LIST) {
        throw new TypeError("document listing metadata is malformed");
    }
    var fleet = normalizeDocumentIdentity(identity);
    return { documents: body.documents.map(function (row) { return listingRow(row, fleet); }) };
}

/** Translate the existing local route's richer rows, dropping its private HTTP address. */
export function localDocumentListing(body, identity) {
    if (!body || typeof body !== "object" || Array.isArray(body) ||
        Object.keys(body).length !== 1 || !Array.isArray(body.documents) ||
        body.documents.length > DOCUMENT_MAX_LIST) {
        throw new TypeError("local document listing is malformed");
    }
    var rows = body.documents.map(function (row) {
        if (!row || typeof row !== "object" || Array.isArray(row) ||
            typeof row.url !== "string" || row.url.charAt(0) !== "/" ||
            row.label !== row.path) {
            throw new TypeError("local document metadata is malformed");
        }
        var expected = row.source === "task"
            ? ["source", "path", "label", "bytes", "modified", "url", "task"]
            : ["source", "path", "label", "bytes", "modified", "url"];
        var keys = Object.keys(row);
        if (keys.length !== expected.length || keys.some(function (key) {
            return expected.indexOf(key) < 0;
        })) throw new TypeError("local document metadata carries an unknown field");
        var out = { scope: row.source, path: row.path, bytes: row.bytes, modified: row.modified };
        if (row.source === "task") {
            if (!row.task || typeof row.task !== "object" || Array.isArray(row.task) ||
                Object.keys(row.task).length !== 2) {
                throw new TypeError("local task document metadata is malformed");
            }
            out.task = row.task.id;
            out.title = row.task.title;
        }
        return out;
    });
    return documentListing({ documents: rows }, identity);
}

function answerKeys(scope) {
    return scope === "task"
        ? ["scope", "task", "path", "media_type", "byte_count", "data"]
        : ["scope", "path", "media_type", "byte_count", "data"];
}

/** Decode one encrypted answer only after its media type, identity and count agree. */
export function documentAnswer(wanted, body) {
    var locator = normalizeDocumentLocator(wanted);
    if (!body || typeof body !== "object" || Array.isArray(body)) {
        throw new TypeError("document answer is malformed");
    }
    var allowed = answerKeys(body.scope);
    var keys = Object.keys(body);
    if (keys.length !== allowed.length || keys.some(function (key) { return allowed.indexOf(key) < 0; }) ||
        body.scope !== locator.scope || body.path !== locator.path ||
        (locator.scope === "task" && body.task !== locator.task) ||
        (body.media_type !== "text/markdown; charset=utf-8" &&
         body.media_type !== "text/plain; charset=utf-8") ||
        !Number.isSafeInteger(body.byte_count) || body.byte_count < 0 ||
        body.byte_count > DOCUMENT_MAX_BYTES || typeof body.data !== "string") {
        throw new TypeError("document answer does not match the requested document");
    }
    var bytes;
    try { bytes = base64Bytes(body.data, "document data"); }
    catch (error) { throw new TypeError("document answer data is not canonical base64"); }
    if (bytes.length !== body.byte_count) {
        throw new TypeError("document answer does not carry the bytes it counts");
    }
    return documentBytesAnswer(locator, body.media_type, bytes);
}

/** Validate bytes from the local HTTP route against the same boundary as an encrypted answer. */
export function documentBytesAnswer(wanted, mediaType, value) {
    var locator = normalizeDocumentLocator(wanted);
    var bytes = value instanceof Uint8Array ? value : new Uint8Array(value || []);
    if (mediaType !== "text/markdown; charset=utf-8" &&
        mediaType !== "text/plain; charset=utf-8") {
        throw new TypeError("document answer has an unsupported media type");
    }
    if (bytes.length > DOCUMENT_MAX_BYTES) {
        throw new TypeError("document answer is too large");
    }
    var text;
    try { text = new TextDecoder("utf-8", { fatal: true }).decode(bytes); }
    catch (error) { throw new TypeError("document answer is not UTF-8 text"); }
    return Object.assign({}, locator, {
        media_type: mediaType, byte_count: bytes.length, text: text
    });
}

/** Prefer the native mobile share sheet; a cancelled/unavailable sheet falls back to copy. */
export async function shareDocument(locator, title, environment) {
    environment = environment || {};
    var nav = environment.navigator || globalThis.navigator;
    var origin = environment.origin || globalThis.location.origin;
    var url = documentShareURL(origin, locator);
    if (nav && typeof nav.share === "function") {
        try { await nav.share({ title: title || locator.path, url: url }); return "shared"; }
        catch (error) { /* clipboard is the deliberate user-gesture fallback */ }
    }
    if (!nav || !nav.clipboard || typeof nav.clipboard.writeText !== "function") {
        var unavailable = new Error("This browser cannot share or copy the document link.");
        unavailable.code = "document_share_unavailable";
        throw unavailable;
    }
    await nav.clipboard.writeText(url);
    return "copied";
}
