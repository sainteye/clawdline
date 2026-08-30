#!/usr/bin/env node
import { readFileSync, readdirSync, statSync } from "node:fs";
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

console.log(`${failed ? "not ok" : "ok"}: ${checks} documented UI-label checks`);
if (failed) process.exit(1);
