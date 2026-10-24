const ENVELOPE_FIELDS = Object.freeze([
    "v", "ch", "seq", "ts", "class", "key_id", "nonce", "ct", "sender", "sig"
]);
const encoder = new TextEncoder();
export const MAX_CONTENT_KEY_ID_BYTES = 128;

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
    if (!validToken(envelope.key_id, MAX_CONTENT_KEY_ID_BYTES)) throw new TypeError("bad key_id");
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

async function verificationKey(envelope, keyOrResolver) {
    var key = typeof keyOrResolver === "function"
        ? await keyOrResolver(envelope.sender, envelope) : keyOrResolver;
    if (!key) return null;
    return typeof key === "string" || key instanceof ArrayBuffer || ArrayBuffer.isView(key)
        ? importSenderPublicKey(key) : key;
}

/**
 * `probe`, when given, is written and never read: `cause` keeps the exception a `false` answer
 * stands for, so a receive-failure row can name it (`cloud-viewer-events.js`). The answer is the
 * same with or without it.
 */
export async function verifyEnvelope(envelope, keyOrResolver, probe) {
    try {
        validateEnvelope(envelope);
        var key = await verificationKey(envelope, keyOrResolver);
        if (!key) return false;
        return subtle().verify({ name: "Ed25519" }, key, base64Bytes(envelope.sig, "sig"),
            envelopeSigningBytes(envelope));
    } catch (e) {
        if (probe) probe.cause = e;
        return false;
    }
}

/**
 * `probe`, when given, has `stage` set immediately before each step, so whatever this throws is
 * attributed to the step that threw it rather than guessed from its message. Observation only:
 * the steps, their order and what is thrown are unchanged.
 */
export async function openEnvelope(envelope, masterKey, keyOrResolver, probe) {
    var mark = function (stage) { if (probe) probe.stage = stage; };
    mark("validate");
    validateEnvelope(envelope);
    mark("signature_verify");
    if (!await verifyEnvelope(envelope, keyOrResolver, probe)) throw new Error("bad envelope signature");
    mark("master_key_lookup");
    var key = typeof masterKey === "string" || masterKey instanceof ArrayBuffer ||
        ArrayBuffer.isView(masterKey) ? await importMasterSecret(masterKey) : masterKey;
    mark("decrypt");
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

/**
 * Persist the complete result of one pairing handover in a single IndexedDB
 * transaction. `key_id` is not secret, but it is part of the envelope identity:
 * losing it while retaining the AES key produces a socket that is authenticated
 * and connected but cannot decrypt any payload.
 */
export async function storePairingCryptoKeys(value, indexedDBValue) {
    if (!value || !value.masterKey || value.masterKey.extractable !== false ||
        !value.senderKey || value.senderKey.extractable !== false) {
        throw new TypeError("only non-extractable pairing CryptoKeys are stored");
    }
    if (!validToken(value.keyID, MAX_CONTENT_KEY_ID_BYTES)) throw new TypeError("bad pairing key id");
    ["masterName", "masterKeyIDName", "senderName"].forEach(function (field) {
        if (typeof value[field] !== "string" || !value[field]) {
            throw new TypeError("pairing key storage name is missing");
        }
    });
    if (value.binding !== undefined) {
        validatePairingBinding(value.binding);
        if (value.binding.keyID !== value.keyID) {
            throw new TypeError("pairing binding key id does not match the stored key");
        }
    }
    if (value.binding !== undefined &&
        (typeof value.bindingName !== "string" || !value.bindingName)) {
        throw new TypeError("pairing binding storage name is missing");
    }
    var preserve = value.preserve || null;
    if (preserve) {
        ["masterName", "masterKeyIDName", "senderName"].forEach(function (field) {
            if (typeof preserve[field] !== "string" || !preserve[field]) {
                throw new TypeError("preserved pairing storage name is missing");
            }
        });
    }
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readwrite");
        var keys = tx.objectStore("keys");
        if (value.binding !== undefined) {
            var previous = keys.get(value.bindingName);
            previous.onsuccess = function () {
                var old = previous.result;
                var floor = old && (old.epoch || (old.keyID === "ms-1" ? 1 : 0));
                var next = value.binding.epoch || (value.keyID === "ms-1" ? 1 : 0);
                if (floor && (next < floor || (next === floor && old.keyID !== value.keyID))) tx.abort();
            };
        }
        keys.put(value.masterKey, value.masterName);
        keys.put(value.keyID, value.masterKeyIDName);
        keys.put(value.senderKey, value.senderName);
        if (value.binding !== undefined) keys.put(Object.assign({}, value.binding,
            { revision: crypto.randomUUID() }), value.bindingName);
        // The first machine paired by a browser also seeds the old account-level names so an
        // already-shipped client remains usable. A later independently paired machine must never
        // replace those names: that would make the first Mac unreadable at the instant AWS pairs.
        if (preserve) {
            var existing = keys.get(preserve.masterName);
            existing.onsuccess = function () {
                if (existing.result !== undefined && existing.result !== null) return;
                keys.put(value.masterKey, preserve.masterName);
                keys.put(value.keyID, preserve.masterKeyIDName);
                keys.put(value.senderKey, preserve.senderName);
            };
            existing.onerror = function () { try { tx.abort(); } catch (e) { /* already failed */ } };
        }
        tx.oncomplete = function () { db.close(); resolve(value); };
        tx.onerror = function () { db.close(); reject(tx.error); };
        tx.onabort = tx.onerror;
    });
}

function validatePairingBinding(value) {
    if (!value || typeof value !== "object" || Array.isArray(value) || value.v !== 1 ||
        typeof value.legacy !== "boolean" || !validToken(value.machineID, 128) ||
        !validToken(value.senderID, 128) || !validToken(value.keyID, MAX_CONTENT_KEY_ID_BYTES) ||
        (value.epoch !== undefined && (!Number.isSafeInteger(value.epoch) || value.epoch < 1))) {
        throw new TypeError("bad pairing machine binding");
    }
    return value;
}

/** Persist the non-secret result of an authenticated legacy-key migration. */
export async function storePairingBinding(name, binding, indexedDBValue) {
    if (typeof name !== "string" || !name) throw new TypeError("pairing binding storage name is missing");
    validatePairingBinding(binding);
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readwrite");
        var keys = tx.objectStore("keys");
        var previous = keys.get(name);
        previous.onsuccess = function () {
            var old = previous.result;
            var floor = old && (old.epoch || (old.keyID === "ms-1" ? 1 : 0));
            var next = binding.epoch || (binding.keyID === "ms-1" ? 1 : 0);
            // An old tab's legacy migration cannot replace a directly paired pin or roll back
            // its durable floor after another tab completed a pairing or rekey.
            if (old && ((old.legacy === false && binding.legacy) || floor > next ||
                (floor && floor === next && old.keyID !== binding.keyID))) { tx.abort(); return; }
            keys.put(binding, name);
        };
        tx.oncomplete = function () { db.close(); resolve(binding); };
        tx.onerror = function () { db.close(); reject(tx.error); };
        tx.onabort = tx.onerror;
    });
}

export async function loadPairingBinding(name, indexedDBValue) {
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readonly");
        var request = tx.objectStore("keys").get(name);
        request.onsuccess = function () {
            db.close();
            if (request.result === undefined || request.result === null) return resolve(null);
            try { resolve(validatePairingBinding(request.result)); }
            catch (error) { reject(error); }
        };
        request.onerror = function () { db.close(); reject(request.error); };
    });
}

/** All machine state is read in one snapshot; a different tab cannot mix key and floor. */
export async function loadMachineCryptoState(names, indexedDBValue) {
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readonly"), keys = tx.objectStore("keys"), state = {};
        ["bindingName", "masterName", "masterKeyIDName", "senderName"].forEach(function (name) {
            var request = keys.get(names[name]);
            request.onsuccess = function () { state[name] = request.result; };
        });
        tx.oncomplete = function () { db.close(); resolve(state); };
        tx.onerror = function () { db.close(); reject(tx.error); };
        tx.onabort = tx.onerror;
    });
}

/** Compare the latest durable floor and consume the operation in the same key transaction.
 * No await occurs inside the transaction. The sole pending operation lives in the caller;
 * a restart loses it and cannot call this path from an unsolicited answer. */
export async function adoptMachineRekey(value, indexedDBValue) {
    if (!value.masterKey || value.masterKey.extractable !== false || !validToken(value.keyID, MAX_CONTENT_KEY_ID_BYTES) ||
        !Number.isSafeInteger(value.epoch) || value.epoch < 1 || typeof value.valid !== "function") {
        throw new TypeError("bad rekey adoption");
    }
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readwrite"), keys = tx.objectStore("keys");
        var refused = new Error("content key exchange rejected");
        refused.code = "unknown_key";
        var adopted;
        var request = keys.get(value.bindingName);
        request.onsuccess = function () {
            var b = request.result;
            var floor = b && (b.epoch === undefined && b.keyID === "ms-1" ? 1 : b.epoch);
            if (!b || b.legacy !== false || b.machineID !== value.expected.machineID ||
                b.senderID !== value.expected.senderID || b.revision !== value.expected.revision ||
                !Number.isSafeInteger(floor) || floor < 1 || floor > value.epoch ||
                (floor === value.epoch && b.keyID !== value.keyID)) {
                refused.code = "machine_pairing_changed";
                tx.abort(); return;
            }
            if (b.lastRekeyOperation === value.operation || !value.valid()) {
                tx.abort(); return;
            }
            adopted = Object.assign({}, b, { keyID: value.keyID, epoch: value.epoch,
                lastRekeyOperation: value.operation });
            keys.put(value.masterKey, value.masterName);
            keys.put(value.keyID, value.masterKeyIDName);
            keys.put(adopted, value.bindingName);
        };
        tx.oncomplete = function () { db.close(); resolve(adopted); };
        tx.onerror = function () { db.close(); reject(refused.code === "machine_pairing_changed" ? refused : tx.error || refused); };
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

export async function loadCryptoKeyID(name, indexedDBValue) {
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readonly");
        var request = tx.objectStore("keys").get(name);
        request.onsuccess = function () {
            db.close();
            var value = request.result;
            if (value === undefined || value === null) return resolve(null);
            if (!validToken(value, MAX_CONTENT_KEY_ID_BYTES)) return reject(new TypeError("bad stored pairing key id"));
            resolve(value);
        };
        request.onerror = function () { db.close(); reject(request.error); };
    });
}

/*
 * The identity handover's result (`cloud/health-owner/handover.ts`): the master secret as a
 * non-extractable HKDF key and AES-GCM key, with the content-key epoch Cloud recorded for it.
 * One record, one put, so a reload can never find the key without its epoch or the reverse.
 * The caller validates the record's shape; this layer refuses anything extractable.
 */
export async function storeHealthHandover(name, record, indexedDBValue) {
    if (typeof name !== "string" || !name) throw new TypeError("handover storage name is missing");
    if (!record || !record.hkdfKey || record.hkdfKey.extractable !== false ||
        !record.masterKey || record.masterKey.extractable !== false) {
        throw new TypeError("only non-extractable handover CryptoKeys are stored");
    }
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readwrite");
        var keys = tx.objectStore("keys");
        var previous = keys.get(name);
        previous.onsuccess = function () {
            var old = previous.result;
            if (old && (!Number.isSafeInteger(old.contentKeyEpoch) || old.contentKeyEpoch < 1 ||
                !Number.isSafeInteger(old.identityEpoch) || old.identityEpoch < 0 ||
                old.accountID !== record.accountID || old.machineID !== record.machineID ||
                record.contentKeyEpoch < old.contentKeyEpoch || record.identityEpoch < old.identityEpoch ||
                (record.contentKeyEpoch === old.contentKeyEpoch && old.contentKeyID && old.contentKeyID !== record.contentKeyID) ||
                (old.machineSigningKey && old.machineSigningKey !== record.machineSigningKey))) {
                tx.abort();
                return;
            }
            keys.put(record, name);
        };
        previous.onerror = function () { tx.abort(); };
        tx.oncomplete = function () { db.close(); resolve(record); };
        tx.onerror = function () { db.close(); reject(tx.error); };
        tx.onabort = tx.onerror;
    });
}

export async function loadHealthHandover(name, indexedDBValue) {
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readonly");
        var request = tx.objectStore("keys").get(name);
        request.onsuccess = function () { db.close(); resolve(request.result || null); };
        request.onerror = function () { db.close(); reject(request.error); };
    });
}

/** Remove every handover record whose name starts with `prefix` (one account's). */
export async function deleteHealthHandovers(prefix, indexedDBValue) {
    if (typeof prefix !== "string" || !prefix) throw new TypeError("handover storage prefix is missing");
    var db = await keyDatabase(indexedDBValue);
    return new Promise(function (resolve, reject) {
        var tx = db.transaction("keys", "readwrite");
        tx.objectStore("keys").delete(IDBKeyRange.bound(prefix, prefix + "￿"));
        tx.oncomplete = function () { db.close(); resolve(); };
        tx.onerror = function () { db.close(); reject(tx.error); };
        tx.onabort = tx.onerror;
    });
}
