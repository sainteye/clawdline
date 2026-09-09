import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const iterm = fs.readFileSync(path.join(root, "Resources/iterm.js"), "utf8");
const settings = fs.readFileSync(path.join(root, "Sources/Settings.swift"), "utf8");
const cloudHandover = fs.readFileSync(
    path.join(root, "Sources/CloudHandover.swift"), "utf8");
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

const cloudControl = settings.slice(
    settings.indexOf("private final class CloudSettingsControl"),
    settings.indexOf("// MARK: - The apps the hotkey fires in"));
assert.match(cloudControl, /Pair a Browser…/,
    "Cloud Settings exposes the browser half of the pairing protocol instead of only phone QR");
const pairBrowser = functionBody(cloudControl, "private func beginBrowserPairing()");
const browserWorkflow = cloudHandover.slice(
    cloudHandover.indexOf("final class CloudBrowserPairingWorkflow"));
assert.match(browserWorkflow, /CloudHandover\.decodeOfferFragment/,
    "the Mac decodes the carried browser offer before asking the person to trust it");
const confirmationStart = pairBrowser.indexOf("let confirmation = NSAlert()");
const confirmationEnd = pairBrowser.indexOf("pairingKind = .browser");
const confirmation = pairBrowser.slice(confirmationStart, confirmationEnd);
assert.ok(confirmationStart >= 0 && confirmationEnd > confirmationStart,
    "browser pairing has a closed confirmation step before its async handover starts");
assert.match(confirmation, /confirmation\.informativeText[\s\S]*preview\.viewerFingerprint/,
    "the consent dialog itself shows the decoded browser fingerprint");
assert.match(confirmation,
    /guard confirmation\.runModal\(\) == \.alertFirstButtonReturn else \{ return \}/,
    "only the explicit Pair Browser button crosses the consent boundary");
assert.match(browserWorkflow, /CloudPairingCompleter = \.production\(\)/,
    "browser pairing reuses the encrypted Cloud handover rather than minting another credential");
assert.match(browserWorkflow, /\.complete\(offerFragment:/,
    "the accepted browser offer is delivered through the existing bounded completer");
assert.doesNotMatch(pairBrowser + browserWorkflow,
    /Log\.write|NSLog|os_log|Logger\s*\(|Diagnostics\.|print\(/,
    "the carried offer is never copied into a diagnostic or console log");
const connected = cloudControl.slice(
    cloudControl.indexOf("case .connected"), cloudControl.indexOf("case .signingOut"));
assert.match(connected, /if pairing[\s\S]*Cancel Pairing/,
    "an in-flight handover exposes one explicit cancellation action");
assert.doesNotMatch(connected,
    /if pairing[\s\S]*Cancel Pairing[\s\S]*Sign Out[\s\S]*else/,
    "Sign Out is not available in the in-flight pairing action set");
const cloudDeinit = functionBody(cloudControl, "deinit");
assert.match(cloudDeinit, /pairingTask\?\.cancel\(\)/,
    "tearing down Cloud Settings cancels its phone pairing task");
assert.match(cloudControl, /private var browserPairing: CloudBrowserPairingWorkflow\?/,
    "Cloud Settings owns the browser pairing workflow for exactly its own lifetime");
const workflowDeinit = functionBody(browserWorkflow, "deinit");
assert.match(workflowDeinit, /task\?\.cancel\(\)/,
    "releasing Cloud Settings releases the workflow, whose own deinit cancels its task");
const byteLimit = pairBrowser.indexOf("maxOfferFragmentUTF8Bytes");
const preview = pairBrowser.indexOf("workflow.preview(raw:");
assert.ok(byteLimit >= 0 && preview > byteLimit,
    "the carried offer is byte-bounded before any base64 or JSON decoding");

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
