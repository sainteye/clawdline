import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import vm from "node:vm";

const page = readFileSync("Resources/web/index.html", "utf8");
const diagnostics = readFileSync("Resources/web/app/js/core/layout-diagnostics.js", "utf8");
const detailActions = readFileSync("Resources/web/app/js/input/detail-actions.js", "utf8");
const work = mkdtempSync(join(tmpdir(), "clawdline-web-usage-"));
const fixture = join(work, "index.html");

function guard(tool, source) {
  writeFileSync(fixture, source);
  return spawnSync("python3", [tool], {
    cwd: process.cwd(),
    env: { ...process.env, CLAWDLINE_WEB_INDEX: fixture },
    encoding: "utf8",
  });
}

try {
  const header = page.slice(page.indexOf('<header class="top">'), page.indexOf("</header>"));
  const settings = page.slice(page.indexOf('id="settings"'), page.indexOf('id="start"'));
  assert.doesNotMatch(header, /id="usage-open"/,
                      "Usage must not remain a standalone header action");
  assert.match(settings, /id="usage-open"/,
               "the Logo settings menu must contain the Usage action");
  assert.doesNotMatch(diagnostics, /installDebugButton/,
                      "the connection button must not install the retired layout Debug action");
  assert.match(detailActions, /els\.conn\.addEventListener\("click"[\s\S]*api\.refresh/,
               "the retained connection button must refresh or reconnect when pressed");

  for (const tool of ["tools/check-web-ids.py", "tools/check-web-strings.py"]) {
    const result = guard(tool, page);
    assert.equal(result.status, 0, `${tool} baseline failed: ${result.stdout}${result.stderr}`);
  }

  const renamed = page.replace('id="usage-token-body"', 'id="usage-token-body-renamed"');
  assert.notEqual(renamed, page, "usage-token-body mutation did not change the fixture");
  const missingID = guard("tools/check-web-ids.py", renamed);
  assert.notEqual(missingID.status, 0, "renaming usage-token-body must make the id guard RED");
  assert.match(missingID.stdout + missingID.stderr, /usage-token-body/);

  const begin = page.indexOf("// Usage Analytics IIFE: begin");
  const endMarker = "// Usage Analytics IIFE: end";
  const end = page.indexOf(endMarker, begin);
  assert.ok(begin >= 0 && end > begin, "Usage Analytics IIFE mutation boundaries must exist");
  const deleted = page.slice(0, begin) + page.slice(end + endMarker.length);
  for (const tool of ["tools/check-web-ids.py", "tools/check-web-strings.py"]) {
    const result = guard(tool, deleted);
    assert.notEqual(result.status, 0, `deleting the Usage Analytics IIFE must make ${tool} RED`);
  }

  class FakeNode {
    constructor() {
      this.children = [];
      this.listeners = {};
      this.style = {};
      this.dataset = {};
      this.hidden = false;
      this.value = "";
      this.textContent = "";
    }
    appendChild(child) { this.children.push(child); return child; }
    removeChild(child) { this.children = this.children.filter((item) => item !== child); }
    get firstChild() { return this.children[0] || null; }
    addEventListener(name, handler) { this.listeners[name] = handler; }
    setAttribute(name, value) { this[name] = value; }
    focus() {}
    remove() {}
    click() {}
  }
  const nodes = new Map();
  const node = (id) => {
    if (!nodes.has(id)) nodes.set(id, new FakeNode());
    return nodes.get(id);
  };
  const body = new FakeNode();
  const fetches = [];
  let blobReads = 0;
  const document = {
    body,
    getElementById: node,
    createElement: () => new FakeNode(),
    addEventListener() {},
  };
  const source = page.slice(begin + "// Usage Analytics IIFE: begin".length, end);
  vm.runInNewContext(source, {
    document,
    fetch: (url) => {
      fetches.push(url);
      return Promise.resolve({
        ok: false,
        status: 413,
        json: () => Promise.resolve({ error: { code: "export_too_large" } }),
        blob: () => { blobReads += 1; return Promise.resolve(new Blob()); },
      });
    },
    Intl,
    Date,
    URL,
    URLSearchParams,
    Blob,
    console,
  });
  nodes.get("usage-export-csv").listeners.click();
  await new Promise((resolve) => setImmediate(resolve));
  await new Promise((resolve) => setImmediate(resolve));
  assert.match(fetches[0], /^\/v1\/orchestrator\/usage\/analytics\.csv\?/);
  assert.equal(blobReads, 0, "a non-2xx export must never be read into a download Blob");
  assert.match(nodes.get("usage-status").textContent, /exceeds the matched-row export limit/);
  assert.notEqual(new Intl.NumberFormat(undefined, { maximumSignificantDigits: 6 }).format(0.004),
                  "0", "sub-cent values must retain significant digits");

  console.log("web usage analytics guards: 15 checks passed");
} finally {
  rmSync(work, { recursive: true, force: true });
}
