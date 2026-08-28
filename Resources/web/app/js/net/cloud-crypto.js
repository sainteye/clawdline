const ENVELOPE_FIELDS = Object.freeze([
    "v", "ch", "seq", "ts", "class", "key_id", "nonce", "ct", "sender", "sig"
]);
const encoder = new TextEncoder();

function subtle() {
    if (!globalThis.crypto || !globalThis.crypto.subtle) {
        throw new Error("WebCrypto is unavailable");
    }
    return globalThis.crypto.subtle;
}

export function base64Bytes(text, field) {
    if (typeof text !== "string" || text.length % 4 !== 0 ||
        !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(text)) {
        throw new TypeError((field || "value") + " is not canonical base64");
    }
    var binary;
    try { binary = atob(text); } catch (e) {
        throw new TypeError((field || "value") + " is not canonical base64");
    }
    var bytes = new Uint8Array(binary.length);
    for (var i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
    if (bytesBase64(bytes) !== text) {
        throw new TypeError((field || "value") + " is not canonical base64");
    }
    return bytes;
}

export function bytesBase64(value) {
    var bytes = byteView(value);
    var out = "";
    var step = 0x8000;
    for (var i = 0; i < bytes.length; i += step) {
        out += String.fromCharCode.apply(null, bytes.subarray(i, i + step));
    }
    return btoa(out);
}

function byteView(value) {
    if (value instanceof Uint8Array) return value;
    if (value instanceof ArrayBuffer) return new Uint8Array(value);
    if (ArrayBuffer.isView(value)) {
        return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
    }
    if (typeof value === "string") return base64Bytes(value);
    throw new TypeError("expected bytes or canonical base64");
}

function validToken(value, maximum) {
    if (typeof value !== "string" || !value || value.length > maximum) return false;
    for (var i = 0; i < value.length; i += 1) {
        var code = value.charCodeAt(i);
        if (code < 0x21 || code > 0x7e || code === 0x2f || code === 0x7c) return false;
    }
    return true;
}

export function parseEnvelopeChannel(value) {
    if (typeof value !== "string" || !value || value.length > 300) {
        throw new TypeError("ch is empty or too long");
    }
    var parts = value.split("/");
    if (parts[0] === "wh") throw new TypeError("the wh/ channel is reserved");
    for (var i = 1; i < parts.length; i += 1) {
        if (!validToken(parts[i], 128)) throw new TypeError("ch has an unusable segment");
    }
    if ((parts[0] === "s" || parts[0] === "t") && parts.length === 3) {
        return { kind: parts[0] === "s" ? "session" : "transcript",
            machine: parts[1], session: parts[2] };
    }
    if ((parts[0] === "orch" || parts[0] === "ctl") && parts.length === 2) {
        return { kind: parts[0], machine: parts[1] };
    }
    if (parts[0] === "ho" && parts.length === 3) {
        return { kind: "handoff", account: parts[1], handoff: parts[2] };
    }
    throw new TypeError("unknown or malformed channel");
}

export function validateEnvelope(envelope) {
    if (!envelope || typeof envelope !== "object" || Array.isArray(envelope)) {
        throw new TypeError("envelope must be an object");
    }
    var keys = Object.keys(envelope);
    if (keys.length !== ENVELOPE_FIELDS.length ||
        keys.some(function (key) { return ENVELOPE_FIELDS.indexOf(key) < 0; }) ||
        ENVELOPE_FIELDS.some(function (key) { return !(key in envelope); })) {
        throw new TypeError("envelope fields do not match protocol v1");
    }
    if (envelope.v !== 1) throw new TypeError("unsupported envelope version");
    if (!Number.isSafeInteger(envelope.seq) || envelope.seq < 0) throw new TypeError("bad seq");
    if (!Number.isSafeInteger(envelope.ts) || envelope.ts < 0) throw new TypeError("bad ts");
    var channel = parseEnvelopeChannel(envelope.ch);
    var allowed = channel.kind === "ctl" ? ["ctl", "dispatch"] :
        channel.kind === "handoff" ? ["ho"] : ["stream"];
    if (allowed.indexOf(envelope.class) < 0) throw new TypeError("class does not match channel");
    if (!validToken(envelope.key_id, 64)) throw new TypeError("bad key_id");
    if (!validToken(envelope.sender, 128)) throw new TypeError("bad sender");
    if (base64Bytes(envelope.nonce, "nonce").length !== 12) throw new TypeError("bad nonce length");
    if (base64Bytes(envelope.ct, "ct").length < 16) throw new TypeError("empty ciphertext");
    if (base64Bytes(envelope.sig, "sig").length !== 64) throw new TypeError("bad signature length");
    return channel;
}

/** The deployed relay intentionally excludes sender: selecting sender chooses the verification key. */
export function envelopeSigningString(envelope) {
    return [String(envelope.v), envelope.ch, String(envelope.seq), String(envelope.ts),
        envelope.class, envelope.key_id, envelope.nonce, envelope.ct].join("|");
}

export function envelopeSigningBytes(envelope) {
    return encoder.encode(envelopeSigningString(envelope));
}

export async function importMasterSecret(value, usages) {
    var bytes = byteView(value);
    if (bytes.length !== 32) throw new TypeError("the account master secret must be 32 bytes");
    return subtle().importKey("raw", bytes, { name: "AES-GCM" }, false,
        usages || ["encrypt", "decrypt"]);
}

export async function importSenderPublicKey(value) {
    var bytes = byteView(value);
    if (bytes.length !== 32) throw new TypeError("an Ed25519 public key must be 32 bytes");
    return subtle().importKey("raw", bytes, { name: "Ed25519" }, false, ["verify"]);
}

export async function importDevicePrivateKey(value) {
    return subtle().importKey("pkcs8", byteView(value), { name: "Ed25519" }, false, ["sign"]);
}

async function verificationKey(envelope, keyOrResolver) {
    var key = typeof keyOrResolver === "function"
        ? await keyOrResolver(envelope.sender, envelope) : keyOrResolver;
    if (!key) return null;
    return typeof key === "string" || key instanceof ArrayBuffer || ArrayBuffer.isView(key)
        ? importSenderPublicKey(key) : key;
}

export async function verifyEnvelope(envelope, keyOrResolver) {
    try {
        validateEnvelope(envelope);
        var key = await verificationKey(envelope, keyOrResolver);
        if (!key) return false;
        return subtle().verify({ name: "Ed25519" }, key, base64Bytes(envelope.sig, "sig"),
            envelopeSigningBytes(envelope));
    } catch (e) {
        return false;
    }
}

export async function openEnvelope(envelope, masterKey, keyOrResolver) {
    validateEnvelope(envelope);
    if (!await verifyEnvelope(envelope, keyOrResolver)) throw new Error("bad envelope signature");
    var key = typeof masterKey === "string" || masterKey instanceof ArrayBuffer ||
        ArrayBuffer.isView(masterKey) ? await importMasterSecret(masterKey) : masterKey;
    var clear = await subtle().decrypt({ name: "AES-GCM", iv: base64Bytes(envelope.nonce, "nonce"),
        tagLength: 128 }, key, base64Bytes(envelope.ct, "ct"));
    return new Uint8Array(clear);
}

export async function sealEnvelope(fields, plaintext, masterKey, signingKey) {
    if (!signingKey) throw new Error("a device signing key is required");
    var nonce = crypto.getRandomValues(new Uint8Array(12));
    var clear = typeof plaintext === "string" ? encoder.encode(plaintext) : byteView(plaintext);
    var ct = await subtle().encrypt({ name: "AES-GCM", iv: nonce, tagLength: 128 },
        masterKey, clear);
    var envelope = {
        v: 1, ch: fields.ch, seq: fields.seq, ts: fields.ts,
        class: fields.class, key_id: fields.key_id,
        nonce: bytesBase64(nonce), ct: bytesBase64(ct), sender: fields.sender, sig: ""
    };
    var signature = await subtle().sign({ name: "Ed25519" }, signingKey,
        envelopeSigningBytes(envelope));
    envelope.sig = bytesBase64(signature);
    validateEnvelope(envelope);
    return envelope;
}

/* CryptoKey is structured-cloned by IndexedDB without becoming extractable. */
function keyDatabase(indexedDBValue) {
    var factory = indexedDBValue || globalThis.indexedDB;
    if (!factory) return Promise.reject(new Error("IndexedDB is unavailable"));
    return new Promise(function (resolve, reject) {
        var request = factory.open("clawdline-cloud-keys", 1);
        request.onupgradeneeded = function () {
            if (!request.result.objectStoreNames.contains("keys")) request.result.createObjectStore("keys");
        };
        request.onsuccess = function () { resolve(request.result); };
        request.onerror = function () { reject(request.error || new Error("could not open key store")); };
    });
}

export async function storeCryptoKey(name, key, indexedDBValue) {
    if (!key || key.extractable !== false) throw new TypeError("only non-extractable CryptoKeys are stored");
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readwrite");
        tx.objectStore("keys").put(key, name);
        tx.oncomplete = function () { db.close(); resolve(key); };
        tx.onerror = function () { db.close(); reject(tx.error); };
        tx.onabort = tx.onerror;
    });
}

export async function loadCryptoKey(name, indexedDBValue) {
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readonly");
        var request = tx.objectStore("keys").get(name);
        request.onsuccess = function () { db.close(); resolve(request.result || null); };
        request.onerror = function () { db.close(); reject(request.error); };
    });
}
