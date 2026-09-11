import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = fs.readFileSync(path.join(root, "Sources/HTTPReliability.swift"), "utf8");

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

function validate(sender, peerClose, finish, timeout) {
    assert.match(sender, /contentContext:\s*\.finalMessage/,
        "ordinary HTTP responses must use Network.framework's final message context");
    assert.match(sender, /isComplete:\s*true/,
        "the final message must be complete so TCP emits an orderly write-close");
    assert.match(sender, /makeDeadline[\s\S]*responseCloseGraceSecondLimit[\s\S]*responseTimedOut/,
        "a peer that never closes must have a bounded reclamation backstop");
    assert.match(sender, /self\.awaitPeerClose\(on: connection\)/,
        "successful sends must wait for the peer close before local cancellation");
    assert.match(peerClose, /connection\.receive[\s\S]*done[\s\S]*self\.finishNetworkResponse/,
        "peer EOF must release the ordinary HTTP connection");
    assert.match(peerClose, /self\.awaitPeerClose\(on: connection\)/,
        "bytes already in flight must be drained until peer EOF");
    assert.match(finish, /cancelResponseDeadline[\s\S]*connection\.cancel\(\)/,
        "peer close must cancel both the deadline lease and the Network connection");
    assert.match(timeout, /responseDeadlines\.removeValue[\s\S]*connection\.cancel\(\)/,
        "the bounded backstop must release its lease and cancel the Network connection");
}

const sender = functionBody(source,
    "func sendResponse(_ bytes: Data, on connection: NWConnection)");
const peerClose = functionBody(source,
    "private func awaitPeerClose(on connection: NWConnection)");
const finish = functionBody(source,
    "private func finishNetworkResponse(_ id: ConnectionID, connection: NWConnection)");
const timeout = functionBody(source,
    "private func responseTimedOut(_ id: ConnectionID, connection: NWConnection)");
validate(sender, peerClose, finish, timeout);

// Mutation proof: each part of the write-close is necessary, and this guard sees its removal.
for (const broken of [
    sender.replace(".finalMessage", ".defaultMessage"),
    sender.replace("isComplete: true", "isComplete: false"),
    sender.replace("self.awaitPeerClose(on: connection)", "connection.cancel()"),
]) {
    assert.throws(() => validate(broken, peerClose, finish, timeout));
}
assert.throws(() => validate(sender,
    peerClose.replace("self.awaitPeerClose(on: connection)", "connection.cancel()"),
    finish, timeout));

console.log("remote response transport: FIN, peer-close drain, and bounded reclamation");
