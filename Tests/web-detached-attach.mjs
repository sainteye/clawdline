/**
 * Where a detached tmux session went, on the page that started it.
 *
 * A session opened in a tmux server Clawdline started for itself is real, listed and drivable,
 * and at the Mac it is drawn nowhere at all: nobody sees it until `tmux attach -t clawdline` is
 * typed. The Mac now says so on the start reply (`attach`), and this is the page's half of that
 * contract — the sentence, the hole the command goes in, and the band it is written on.
 *
 * **The band is the whole of the interesting part.** Everything else it says is about a session
 * on its way, so the row arriving is the end of it. This one is about where the session went,
 * which is still true afterwards and is a command somebody has to carry to a keyboard — so it
 * has to survive both the fifteen-second timeout and the arrival, and leave only when the × is
 * pressed.
 */
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { T, fill } from "../Resources/web/app/js/core/i18n.js";

const ATTACH = "tmux attach -t clawdline";

/* ---- the sentence ------------------------------------------------------- */

assert.equal(typeof T.webStartDetached, "string",
    "core/i18n.js carries the English fallback for a page whose /v1/strings request failed");
assert.ok(T.webStartDetached.includes("{command}"),
    "the command is a hole the reply fills, not a session name spelled out in fourteen places");

const said = fill(T.webStartDetached, { command: ATTACH });
assert.ok(said.includes(ATTACH),
    "filling it puts the Mac's own attach command into the sentence");
assert.ok(!said.includes("{command}"),
    "and leaves no hole behind for a reader to see");
assert.ok(!/refus|could not|failed/i.test(T.webStartDetached),
    "it is a success and must not read as one of the refusals it sits beside");

/* ---- the page ----------------------------------------------------------- */

const start = await readFile(
    new URL("../Resources/web/app/js/input/start.js", import.meta.url), "utf8");

assert.ok(start.includes("began(d && d.id, place, makeClawdfather, d && d.attach)"),
    "a fresh start hands the reply's own attach field on rather than deciding for itself");
assert.ok(/began\(d && d\.id, \{ label: row\.title[\s\S]{0,120}d && d\.attach\)/.test(start),
    "and so does picking a conversation back up, which takes the same route's success arm");
assert.ok(!start.includes(ATTACH),
    "the page never spells the command out: the session name belongs to Tmux.attachCommand");
assert.ok(start.includes("fill(T.webStartDetached, { command: detached })"),
    "the sentence is drawn from /v1/strings with the reply written into its hole");

// Three moments, and the same line has to survive all of them.
assert.ok(start.includes("band(detached ? detachedWords() : T.webStartWaiting, false)"),
    "the wait itself says where the session went instead of waiting for it to appear");
assert.ok(start.includes("band(detached ? detachedWords() : T.webStartSlow, true)"),
    "the fifteen-second timeout does not replace it with advice to look at the Mac");
assert.ok(start.includes("if (detached) band(detachedWords(), true); else hideBand();"),
    "and the row arriving does not take it off the screen");
assert.ok(/dismiss: function \(\) \{\n\s+wait = null;\n\s+detached = null;/.test(start),
    "the × is what ends it, and it clears the command with the wait");

/* ---- the fixture -------------------------------------------------------- */

const mock = await readFile(
    new URL("../Resources/web/app/js/net/mock.js", import.meta.url), "utf8");
assert.ok(mock.includes('MOCK_START === "detached"'),
    "?mock=1&start=detached exists, so this path can be looked at without uninstalling tmux");
assert.ok(mock.includes('attach: detached ? "tmux attach -t clawdline" : ""'),
    "and the fixture sends the field in the shape the server sends it, empty and not absent");

console.log("web detached attach tests passed");
process.exit(0);
