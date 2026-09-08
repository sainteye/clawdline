import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const iterm = fs.readFileSync(path.join(root, "Resources/iterm.js"), "utf8");
const settings = fs.readFileSync(path.join(root, "Sources/Settings.swift"), "utf8");
const readinessSource = fs.readFileSync(
    path.join(root, "Sources/LocalBrowserReadiness.swift"), "utf8");
const remoteServer = fs.readFileSync(path.join(root, "Sources/RemoteServer.swift"), "utf8");
const browserPage = fs.readFileSync(path.join(root, "Resources/web/index.html"), "utf8");

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

const currentTargetSource = functionBody(iterm, "function currentTarget(id, role, pane)");
const context = {};
vm.runInNewContext(`${currentTargetSource}; this.currentTarget = currentTarget;`, context);
assert.deepEqual({ ...context.currentTarget("ABC", "", "") }, { ok: true, id: "ABC" },
    "an ordinary iTerm session keeps its own identity");
assert.deepEqual({ ...context.currentTarget("MIRROR", "client", "356") },
    { ok: true, id: "%356" },
    "the selected iTerm tmux mirror becomes the pane identity used by the combined inventory");
assert.deepEqual({ ...context.currentTarget("MIRROR", "client", "%356") },
    { ok: true, id: "MIRROR" },
    "a malformed pane value is not promoted into a target identity");

const current = functionBody(iterm, "if (cmd === \"current\")");
assert.match(current, /session\.tmuxRole/,
    "the live current-session reading asks whether the selected row is a tmux mirror");
assert.match(current, /session\.tmuxWindowPane/,
    "the live current-session reading asks which pane the mirror draws");
assert.match(current, /currentTarget\(/,
    "the live current-session reading uses the tested identity mapping");

const openRemote = functionBody(settings, "private func openRemote()");
const wait = openRemote.indexOf("whenReadyForBrowser");
const mint = openRemote.indexOf("RemoteAuth.addDevice");
const open = openRemote.indexOf("NSWorkspace.shared.open");
assert.ok(wait >= 0 && mint > wait && open > mint,
    "the browser waits for the listener before a credential is minted and the URL is opened");

const readiness = functionBody(readinessSource, "func whenReadyForBrowser");
assert.match(readiness, /attempt\(/,
    "the public browser gate enters the loopback readiness check");
const polling = functionBody(readinessSource, "private static func attempt");
assert.match(polling, /case \.ready:/,
    "only Network.framework's ready state releases a browser open");
assert.match(polling, /asyncAfter/,
    "a listener still starting is waited for rather than read as ready");
assert.match(polling, /deadline/,
    "a listener that never becomes ready cannot leave the browser action pending forever");

const tokenAdoption = remoteServer.slice(
    remoteServer.indexOf('if let token = request.query["t"]'),
    remoteServer.indexOf('return RemotePage.page(for: request)',
        remoteServer.indexOf('if let token = request.query["t"]')) +
        'return RemotePage.page(for: request)'.length);
assert.match(tokenAdoption, /RemotePage\.page\(for: request\)/,
    "a valid browser token is adopted on a successful document response");
assert.doesNotMatch(tokenAdoption, /status\s*=\s*303|headers\["Location"\]/,
    "token adoption does not depend on Chrome following a connection-closing redirect");
assert.match(browserPage, /searchParams\.delete\(["']t["']\)/,
    "the document removes the adopted credential from its query string");
assert.match(browserPage, /history\.replaceState/,
    "the credential-bearing history entry is replaced rather than retained");

console.log("terminal current target and browser readiness contracts");
