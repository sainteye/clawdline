import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

import {
    fastModeCommand,
    nextFastMode,
    settledFastMode,
} from "../Resources/web/app/js/input/fast-mode.js";

assert.equal(nextFastMode("standard"), "fast",
    "a standard Codex session can be switched to Fast mode");
assert.equal(nextFastMode("fast"), "standard",
    "a Fast Codex session can be switched back to standard");
assert.equal(nextFastMode("unknown"), null,
    "an unknown reading is not permission to guess which direction /fast moves");
assert.equal(fastModeCommand(), "/fast",
    "the switch uses Codex's own closed slash command");
assert.equal(settledFastMode("fast", "fast"), true,
    "read-back settles when the rollout reaches the requested mode");
assert.equal(settledFastMode("fast", "standard"), false,
    "the old rollout reading leaves the requested mode pending");
assert.equal(settledFastMode(null, "fast"), true,
    "with no request pending there is nothing to confirm");

const info = await readFile(
    new URL("../Resources/web/app/js/input/info.js", import.meta.url), "utf8");
assert.match(info, /data-fast-mode=/,
    "Session info renders Fast mode as an explicit control");
assert.match(info, /api\.send\(id, fastModeCommand\(\), \[\]\)/,
    "the control sends /fast through the ordinary authenticated Session write");
assert.match(info, /readFastModeBack\(id, 5\)/,
    "the send receipt is followed by rollout read-back instead of being called confirmation");
assert.match(info, /fastModePending \|\| nextFastMode\(current\) !== target/,
    "a pending toggle cannot be sent a second time and reverse the requested change");

console.log("web Fast mode: 11 checks passed");
