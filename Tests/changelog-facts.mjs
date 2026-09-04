#!/usr/bin/env node
// Nothing watched `CHANGELOG.md`. Until this file existed, `git grep -l CHANGELOG -- Tests tools
// test.sh .github` returned nothing at all — for the document that becomes the release notes, 40
// entries and ~880 lines of them.
//
// `tools/release.sh` opens with the reason that matters: 0.5.0 was cut two hours before the remote
// feature landed, and for a day the README described a product the only downloadable build did not
// contain. "Nothing caught it, because nothing was watching." On 2026-09-04, four entries in
// `## Unreleased` were still describing `orchestrator_max_grandchildren` and a dispatch tree two
// levels deep, months after the second level came back out. Same shape, found by a person reading
// all forty rather than by anything here.
//
// **What this guards, and what it cannot.** It takes every HTTP route named in `## Unreleased` and
// asserts the server still answers something on that path. That is mechanically decidable and it is
// the failure with teeth: an entry describing a route a user cannot call is a promise the download
// does not keep. What it cannot do is tell *doing* from *mentioning* — an entry naming
// `orchestrator_max_grandchildren` to say the key is dead reads exactly like one naming it as a
// live setting. That distinction is not a pattern's to make, so it is not attempted: a check that
// would go red on its own correction teaches people to edit the check.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const read = (p) => readFileSync(root + p, "utf8");

let checks = 0;
let failed = false;
function check(condition, message) {
  checks += 1;
  if (condition) return;
  failed = true;
  console.error(`FAIL: ${message}`);
}

const changelogPath = process.argv[2] ?? "CHANGELOG.md";
const changelog = changelogPath.startsWith("/")
  ? readFileSync(changelogPath, "utf8")
  : read(changelogPath);

// The `## Unreleased` block only. Released sections describe builds that already went out; a route
// removed after 0.6.0 shipped is history, not a lie.
const start = changelog.indexOf("\n## Unreleased\n");
check(start >= 0, "CHANGELOG.md has an `## Unreleased` section");
const rest = changelog.slice(start + 1);
const end = rest.indexOf("\n## ", 1);
const unreleased = end < 0 ? rest : rest.slice(0, end);

// Routes as they are written in prose: `GET /v1/sessions/:id/shells/:shellId`, `POST /v1/voice`,
// or bare `/v1/health`. Path parameters are `:name`; a trailing `…` marks an abbreviation.
const routes = new Set();
for (const m of unreleased.matchAll(/`(?:(GET|POST|PATCH|DELETE|PUT) )?(\/v1\/[A-Za-z0-9/:._-]+)`/g)) {
  routes.add(m[2].replace(/\/$/, ""));
}
check(routes.size > 0, "the Unreleased block names at least one route (the pattern still matches)");

const server = read("Sources/RemoteServer.swift");

// A route is answered if the server's source contains every literal segment of its path, in order,
// as one string. `:id` segments are holes: the server splits those out rather than writing them.
function isAnswered(route) {
  const literal = route.split("/").filter((s) => s && !s.startsWith(":"));
  // The longest run of literal segments that appear consecutively in the source is what a
  // `case ("GET", "/v1/health")` or a `path.hasSuffix("/skills")` actually spells.
  for (let i = 0; i < literal.length; i += 1) {
    for (let j = literal.length; j > i; j -= 1) {
      const needle = "/" + literal.slice(i, j).join("/");
      if (needle.length > 3 && server.includes(needle)) return true;
    }
  }
  return false;
}

for (const route of [...routes].sort()) {
  check(isAnswered(route), `Unreleased names \`${route}\`, but Sources/RemoteServer.swift answers no such path`);
}

console.log(`${checks} checks${failed ? " — FAILED" : ""} (${routes.size} routes named in Unreleased)`);
process.exit(failed ? 1 : 0);
