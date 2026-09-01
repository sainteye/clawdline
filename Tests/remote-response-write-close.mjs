import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = fs.readFileSync(path.join(root, "Sources/RemoteServer.swift"), "utf8");

function functionBody(text, signature) {
    const start = text.indexOf(signature);
    assert.ok(start >= 0, `${signature} is missing`);
    const open = text.indexOf("{", start + signature.length);
    assert.ok(open >= 0, `${signature} has no body`);
    let depth = 0;
    for (let index = open; index < text.length; index += 1) {
        if (text[index] === "{") depth += 1;
        if (text[index] === "}") depth -= 1;
        if (depth === 0) return text.slice(start, index + 1);
    }
    assert.fail(`${signature} has no closed body`);
}

function validate(sender, peerClose) {
    assert.match(sender, /contentContext:\s*\.finalMessage/,
        "ordinary HTTP responses must use Network.framework's final message context");
    assert.match(sender, /isComplete:\s*true/,
        "the final message must be complete so TCP emits an orderly write-close");
    assert.match(sender, /asyncAfter[\s\S]*responseCloseGraceSeconds/,
        "a peer that never closes must have a bounded reclamation backstop");
    assert.match(sender, /self\.awaitPeerClose\(on: conn, backstop: backstop\)/,
        "successful sends must wait for the peer close before local cancellation");
    assert.match(peerClose, /conn\.receive[\s\S]*done[\s\S]*conn\.cancel\(\)/,
        "peer EOF must release the ordinary HTTP connection");
    assert.match(peerClose, /self\.awaitPeerClose\(on: conn, backstop: backstop\)/,
        "bytes already in flight must be drained until peer EOF");
}

const sender = functionBody(source,
    "private func send(_ response: Response, on conn: NWConnection)");
const peerClose = functionBody(source,
    "private func awaitPeerClose(on conn: NWConnection, backstop: DispatchWorkItem)");
validate(sender, peerClose);

// Mutation proof: each part of the write-close is necessary, and this guard sees its removal.
for (const broken of [
    sender.replace(".finalMessage", ".defaultMessage"),
    sender.replace("isComplete: true", "isComplete: false"),
    sender.replace("self.awaitPeerClose(on: conn, backstop: backstop)", "conn.cancel()"),
]) {
    assert.throws(() => validate(broken, peerClose));
}
assert.throws(() => validate(sender,
    peerClose.replace("self.awaitPeerClose(on: conn, backstop: backstop)", "conn.cancel()")));

console.log("remote response transport: FIN, peer-close drain, and bounded reclamation");
