#!/usr/bin/env node
// tools/console-tests.mjs [--list] — the console's whole test suite.
//
// `npm test` in web/ runs this, and so do CI and tools/check.sh: one command,
// so that what fails in CI fails on this machine first. Until 2026-10-09 CI
// ran `node --test "src/**/*.test.ts"` and the local check ran the hand-picked
// list in `npm run check`; 75 of CI's 1014 console tests failed there and
// none of them had ever run locally.
//
// The suite is every *.test.ts and *.test.mjs under web/console/src and
// web/console/tools. tools/check-console-tests.mjs fails when a test file
// sits where neither this suite nor `npm run check` would run it.
//
// --list prints the files and runs nothing. Exit: the test runner's, or 1
// when no file was found (an empty suite is not a pass).
import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

export const consoleDir = join(dirname(fileURLToPath(import.meta.url)), "..", "web", "console");

// Where the suite looks, relative to web/console, and what it runs there.
export const suiteRoots = ["src", "tools"];
export const suitePattern = /\.test\.(ts|mjs)$/;

// Directories nothing is ever run from.
export const ignoredDirs = new Set(["node_modules", "dist", ".vite"]);

// walk lists every file under dir whose name matches, as paths relative to
// base with forward slashes, sorted.
export function walk(base, dir, match) {
  const out = [];
  const visit = (abs) => {
    for (const entry of readdirSync(abs, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        if (!ignoredDirs.has(entry.name)) visit(join(abs, entry.name));
      } else if (match.test(entry.name)) {
        out.push(relative(base, join(abs, entry.name)).split(sep).join("/"));
      }
    }
  };
  visit(join(base, dir));
  return out.sort();
}

// suiteFiles is the suite under one web/console directory.
export function suiteFiles(dir = consoleDir) {
  return suiteRoots.flatMap((root) => walk(dir, root, suitePattern));
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const files = suiteFiles();
  if (process.argv.includes("--list")) {
    console.log(files.join("\n"));
    process.exit(0);
  }
  if (files.length === 0) {
    console.error(`console-tests: no test file under ${suiteRoots.join(", ")} in ${consoleDir}; an empty suite is not a pass`);
    process.exit(1);
  }
  console.error(`console-tests: ${files.length} files under web/console/{${suiteRoots.join(",")}}`);
  const run = spawnSync(process.execPath, ["--experimental-strip-types", "--test", ...files], {
    cwd: consoleDir,
    stdio: "inherit",
  });
  process.exit(run.status ?? 1);
}
