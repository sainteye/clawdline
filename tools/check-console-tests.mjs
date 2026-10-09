#!/usr/bin/env node
// tools/check-console-tests.mjs [--web <dir>] — no console test is left unrun.
//
// A test file counts when the full suite (tools/console-tests.mjs, `npm test`)
// or `npm run check` in web/package.json runs it. One anywhere else under
// web/console — beside the sources, in a new directory, with a suffix the
// suite does not match — passes review and never runs, which is how 75 red
// tests went unseen on this machine until 2026-10-09. A file `npm run check`
// names that no longer exists fails too, since that list is kept by hand.
//
// --web points at another copy of web/ (the guard's own test uses one).
// Exit: 0 every test file runs, 1 one does not, 2 could not check.
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { suiteFiles, walk } from "./console-tests.mjs";

const args = process.argv.slice(2);
let webDir = join(dirname(fileURLToPath(import.meta.url)), "..", "web");
if (args[0] === "--web" && args[1]) webDir = args[1];
else if (args.length) {
  console.error("usage: tools/check-console-tests.mjs [--web <dir>]");
  process.exit(2);
}
const consoleDir = join(webDir, "console");

let check;
try {
  check = JSON.parse(readFileSync(join(webDir, "package.json"), "utf8")).scripts?.check;
} catch (err) {
  console.error(`check-console-tests: could not read ${join(webDir, "package.json")}: ${err.message}`);
  process.exit(2);
}
if (typeof check !== "string") {
  console.error("check-console-tests: web/package.json has no \"check\" script to read");
  process.exit(2);
}

// Any name a test runner might pick up, wider than the suite's own pattern,
// so a .test.tsx or .test.js is caught rather than ignored.
const anyTest = /\.test\.(ts|tsx|mts|cts|mjs|js|cjs|jsx)$/;

// The check script's paths are relative to web/ and start with console/.
const named = new Set(
  check.split(/\s+/).filter((word) => anyTest.test(word) && word.startsWith("console/")).map((word) => word.slice("console/".length)),
);
const suite = new Set(suiteFiles(consoleDir));
const all = walk(consoleDir, ".", anyTest).map((p) => p.replace(/^\.\//, ""));

const unrun = all.filter((file) => !suite.has(file) && !named.has(file));
const missing = [...named].filter((file) => !existsSync(join(consoleDir, file)));

for (const file of unrun) {
  console.error(`check-console-tests: web/console/${file} is run by neither \`npm test\` (tools/console-tests.mjs) nor \`npm run check\``);
}
for (const file of missing) {
  console.error(`check-console-tests: \`npm run check\` names web/console/${file}, which does not exist`);
}
if (unrun.length || missing.length) {
  console.error("  Move a test under web/console/src or web/console/tools with a .test.ts or .test.mjs name, or remove the stale name.");
  process.exit(1);
}
console.log(`check-console-tests: all ${all.length} console test files run (${suite.size} in the suite, ${named.size} named by npm run check)`);
