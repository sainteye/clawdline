import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

import { T } from "../Resources/web/app/js/core/i18n.js";
import {
    clawdfatherInstruction,
    clawdfatherRequest
} from "../Resources/web/app/js/input/clawdfather.js";

/* --------------------------------------------------------------------------
   Asking a session to make itself Clawdfather

   The browser holds a device token and never the orchestrator token, so it cannot register a
   coordinator and must not look as though it could. What it can do is type: the same
   `POST /v1/sessions/:id/send` every other composed message already uses. The session on the
   other end is a local process that can read `~/.config/clawdline/orchestrator-token`, so the
   registration is carried out there, by the one caller the broker already trusts with it.

   These are the facts that decision rests on: who the item is offered to, what it says once the
   authenticated `session.coordinator` projection has appeared, and that nothing in the path
   names an orchestrator route.
   -------------------------------------------------------------------------- */

assert.equal(clawdfatherRequest(null).shown, false,
    "no session under the menu offers nothing");

const shell = { id: "shell", label: "a bare prompt" };
const bare = clawdfatherRequest(shell);
assert.equal(bare.shown, false,
    "a session with no assistant is an address that cannot answer a typed instruction");
assert.equal(bare.enabled, false);
assert.equal(bare.state, "unaddressable");

const ordinary = { id: "35D87610-E7F4-4A9A-95A0-11947CF5115C", assistant: "claude",
                   label: "ordinary session" };
const offered = clawdfatherRequest(ordinary);
assert.equal(offered.shown, true,
    "every live addressable session may be asked, not only the bound coordinator's row");
assert.equal(offered.enabled, true);
assert.equal(offered.state, "available");
assert.equal(offered.label, T.webMakeClawdfather);
assert.equal(offered.status, null);

const codex = clawdfatherRequest({ id: "AE8A927C", assistant: "codex", label: "codex tab" });
assert.equal(codex.enabled, true, "Codex tabs are addressable in exactly the same way");

// The authenticated projection is the only thing that says a session already holds the role.
// Reusing it here is what keeps this from inventing a second status-reading path.
const bound = clawdfatherRequest({
    id: "clawdfather", assistant: "claude", label: "Clawdfather",
    coordinator: { label: "Clawdfather", status: "online", commands: [] }
});
assert.equal(bound.state, "bound");
assert.equal(bound.shown, true, "the answer stays on screen rather than vanishing");
assert.equal(bound.enabled, false, "there is nothing to ask a session that already holds it");
assert.equal(bound.status, "online");
assert.match(bound.label, /Clawdfather/);
assert.notEqual(bound.label, T.webMakeClawdfather);

// A label that is not a string, or an empty one, is not an assistant.
assert.equal(clawdfatherRequest({ id: "x", assistant: "   " }).shown, false);
assert.equal(clawdfatherRequest({ id: "x", assistant: 7 }).shown, false);

/* ---- the line that is actually typed -------------------------------------- */

const line = clawdfatherInstruction(ordinary);
assert.ok(line.length > 0);
assert.match(line, /35D87610-E7F4-4A9A-95A0-11947CF5115C/,
    "the browser already knows the terminal-neutral id and hands it over rather than "
    + "leaving the session to work out who it is");
assert.doesNotMatch(line, /\{id\}/, "the hole is filled, not shipped");
assert.match(line, /orchestrator-token/,
    "the session is told which credential the recipe needs, since only it can read one");
assert.equal(clawdfatherInstruction({ assistant: "claude" }), "",
    "no id means no instruction rather than one addressed to nobody");
assert.equal(clawdfatherInstruction(null), "");

/* ---- the browser never reaches for a privileged route --------------------- */

const source = await readFile(
    new URL("../Resources/web/app/js/input/clawdfather.js", import.meta.url), "utf8"
);
// Prose may name the routes and credentials it is explaining; code may not reach for one. So the
// comments come off first, and what is left has no transport in it at all — this module composes
// a sentence and returns it.
const code = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
assert.doesNotMatch(code, /\bfetch\s*\(|\bapi\./,
    "the composer never speaks to the network itself");
assert.doesNotMatch(code, /X-Clawdline-Orchestrator|v1\/orchestrator/,
    "the orchestrator credential and its routes are not something a browser can hold");
assert.match(code, /export function clawdfatherRequest/);

const wiring = await readFile(
    new URL("../Resources/web/app/js/input/detail-actions.js", import.meta.url), "utf8"
);
assert.match(wiring, /clawdfather:\s*function/,
    "the menu action lives beside the other session actions");
assert.match(wiring, /api\.send\(id, text, \[\]\)/,
    "the request is the existing send route and nothing else");
assert.doesNotMatch(wiring, /v1\/orchestrator/);
const askAt = wiring.indexOf("ActionConfirm.open(\"clawdfather\"");
const sendAt = wiring.indexOf("sendClawdfather: function");
assert.ok(askAt >= 0 && sendAt >= 0 && askAt < sendAt,
    "machine-wide state changes only after a second press");

const menu = await readFile(
    new URL("../Resources/web/app/js/input/action-confirm.js", import.meta.url), "utf8"
);
assert.match(menu, /#session-clawdfather/,
    "the menu item is wired through the same click delegate as the other session actions");

const index = await readFile(
    new URL("../Resources/web/index.html", import.meta.url), "utf8"
);
assert.match(index, /id="session-clawdfather"[^>]*type="button"[^>]*role="menuitem"/,
    "the item is a real menu item rather than a decorated div");

const dom = await readFile(
    new URL("../Resources/web/app/js/core/dom.js", import.meta.url), "utf8"
);
assert.match(dom, /"session-clawdfather"/,
    "the element is looked up at load time with the rest of the registry");

/* ---- the recipe the far end is asked to follow ---------------------------- */

// The instruction names a procedure by name. A session that arrives at it and finds nothing there
// is back to reading Swift source, which is the thing this item exists to avoid — so the written
// recipe is part of the feature and is checked with it.
const recipe = await readFile(
    new URL("../docs/orchestrator.md", import.meta.url), "utf8"
);
assert.match(recipe, /### Becoming Clawdfather/,
    "the procedure has a heading somebody can be sent to");
assert.match(recipe, /ITERM_SESSION_ID/);
assert.match(recipe, /CODEX_THREAD_ID/,
    "the id a Codex session would reach for first, and why it is the wrong one");
assert.match(recipe, /POST \/v1\/orchestrator\/coordinator\/register/);
assert.match(recipe, /coordinator\/rebind/);
assert.match(recipe, /expected_generation/);
assert.match(recipe, /coordinator_online/,
    "the refusal that says a live coordinator is not to be taken over");

for (const skill of ["../skills/clawdline/SKILL.md", "../skills/clawdline/SKILL.zh-TW.md"]) {
    const text = await readFile(new URL(skill, import.meta.url), "utf8");
    assert.match(text, /Clawdfather/, `${skill} carries the procedure`);
    assert.match(text, /CODEX_THREAD_ID/, `${skill} names the wrong id`);
    assert.match(text, /expected_generation/, `${skill} names the compare-and-swap value`);
    assert.match(text, /docs\/orchestrator\.md/, `${skill} points at the long form`);
}

// Phase A2's preview-only command list is a separate, still-unresolved feature.
const controls = await readFile(
    new URL("../Resources/web/app/js/input/coordinator-actions.js", import.meta.url), "utf8"
);
assert.match(controls, /confirmDisabled: true/,
    "the Clawdfather command preview gate is untouched by this affordance");

console.log("web clawdfather tests passed");
process.exit(0);
