#!/usr/bin/env node
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, normalize, relative } from "node:path";
const root = new URL("../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");
const english = read("Sources/Copy+English.swift");
const chinese = read("Sources/Copy+Chinese.swift");
const readme = read("README.md");
const readmeZh = read("README.zh-TW.md");
const expected = [
  [english, "Let a browser or your phone see your sessions"],
  [english, "Reach this Mac from anywhere"],
  [english, "Let a paired device write into a session"],
  [chinese, "讓瀏覽器或你的手機看得到你的 session"],
  [chinese, "從任何地方連到這台 Mac"],
  [chinese, "讓配對過的裝置寫進 session"],
];
const stale = ["Answer over HTTP", "Reachable from outside", "Let paired devices type"];

const files = [];
for (const directory of ["docs", "skills"]) {
  const base = new URL(directory + "/", root);
  for (const relative of readdirSync(base, { recursive: true })) {
    const url = new URL(relative, base);
    if (statSync(url).isFile() && /\.(?:md|html)$/.test(relative)) files.push(url);
  }
}
files.push(new URL("tools/shoot-assets.sh", root));

let checks = 0;
let failed = false;
function check(condition, message) {
  checks += 1;
  if (condition) return;
  failed = true;
  console.error(`FAIL: ${message}`);
}

for (const [source, label] of expected) {
  check(source.includes(`"${label}"`), `shipping copy is missing ${JSON.stringify(label)}`);
}
check(readme.includes("https://clawdline.com/docs"), "English README does not link to the canonical public manual");
check(readmeZh.includes("https://clawdline.com/docs"), "Traditional Chinese README does not link to the canonical public manual");
check(!readmeZh.includes("clawdline.com/zh-TW/docs"), "Traditional Chinese README still links to the legacy language-prefixed manual");
for (const [url, label] of [
  ["https://clawdline.com/clawdfather", "Clawdfather"],
  ["https://clawdline.com/pricing", "價格"],
  ["https://clawdline.com/security", "安全性"],
]) {
  check(readmeZh.includes(url), `Traditional Chinese README does not link to the canonical ${label} page`);
}
for (const statement of [
  "## Clawdfather 交付循環",
  "已交付，不等於已審查",
  "已審查，不等於已落地",
  "精確的候選版本",
  "安全關閉必須有證據",
  "本機用量分析",
]) {
  check(readmeZh.includes(statement), `Traditional Chinese README is missing ${JSON.stringify(statement)}`);
}
for (const file of files) {
  const text = readFileSync(file, "utf8");
  for (const label of stale) {
    check(!text.includes(label), `${file.pathname} still documents stale UI label ${JSON.stringify(label)}`);
  }
}

// `artifacts/` is a symlink into a private sibling checkout and is in `.gitignore`, so a link to a
// path inside it resolves perfectly on the machine where the page is written and 404s for every
// reader on GitHub — the mistake is invisible to the person making it, which is the only reason it
// needs a guard rather than care. `docs/handoff.md` carried four such links, three distinct targets,
// from the day `artifacts/` became a symlink until 2026-09-04.
//
// The general form of this check — every relative link target resolves to a file git tracks — was
// measured over the 317 relative links in `docs/`, `skills/`, both READMEs and `CONTRIBUTING.md` on
// 2026-09-04. It finds two more real dead links, both `docs/dispatching.md` writing
// `](docs/orchestrator.md)` from inside `docs/`, which resolves to `docs/docs/…`. That file was
// outside this change's claims, so the guard here is the narrow one; widen it once those are fixed.
const linkScan = [...files, new URL("README.md", root), new URL("README.zh-TW.md", root), new URL("CONTRIBUTING.md", root)];
const rootPath = fileURLToPath(root);
let linksScanned = 0;
for (const file of linkScan) {
  const filePath = fileURLToPath(file);
  const text = readFileSync(filePath, "utf8");
  for (const match of text.matchAll(/\]\(([^)\s]+)\)/g)) {
    const target = match[1];
    if (/^(?:https?:|mailto:|#)/.test(target)) continue;
    linksScanned += 1;
    const resolved = relative(rootPath, normalize(join(dirname(filePath), target.split("#")[0])));
    check(
      !resolved.startsWith("artifacts/") && resolved !== "artifacts",
      `${relative(rootPath, filePath)} links to ${JSON.stringify(target)}, which is inside the git-ignored \`artifacts/\` symlink — it resolves here and 404s for everybody else. Name the file in plain text instead.`,
    );
  }
}
// A scan that matched nothing would pass every check above it and prove nothing at all.
check(linksScanned > 0, "the relative-link scan found no links in any of the documents — the pattern stopped matching, not the documents");

console.log(`${failed ? "not ok" : "ok"}: ${checks} documented UI-label checks`);
if (failed) process.exit(1);
