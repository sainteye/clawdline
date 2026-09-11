import assert from "node:assert/strict";
import { readdir, readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { bindSessionUI, callSessionUI, sessionUICallback } from
    "../Resources/web/app/js/session/ui.js";

const root = fileURLToPath(new URL("../Resources/web/app/js/", import.meta.url));

async function filesUnder(dir) {
    const answer = [];
    for (const name of await readdir(dir)) {
        const full = path.join(dir, name);
        if ((await stat(full)).isDirectory()) answer.push(...await filesUnder(full));
        else if (name.endsWith(".js")) answer.push(full);
    }
    return answer;
}

const files = (await filesUnder(root)).sort();
const known = new Set(files);
const graph = new Map();
const importPattern = /(?:import|export)\s+(?:[^"']*?\s+from\s+)?["']([^"']+)["']/g;
for (const file of files) {
    const source = await readFile(file, "utf8");
    const edges = [];
    for (const match of source.matchAll(importPattern)) {
        if (!match[1].startsWith(".")) continue;
        let target = path.resolve(path.dirname(file), match[1]);
        if (!path.extname(target)) target += ".js";
        if (known.has(target)) edges.push(target);
    }
    graph.set(file, [...new Set(edges)]);
}

// Verification invokes this mode once and expects it to fail. Reintroducing a renderer-facing
// edge from the policy seam to the composition root closes a real two-way import cycle.
if (process.argv.includes("--mutate-cycle")) {
    graph.get(path.join(root, "session/ui.js")).push(path.join(root, "main.js"));
}

let index = 0;
const indices = new Map(), low = new Map(), stack = [], stacked = new Set(), components = [];
function visit(node) {
    indices.set(node, index); low.set(node, index); index += 1;
    stack.push(node); stacked.add(node);
    for (const next of graph.get(node) || []) {
        if (!indices.has(next)) { visit(next); low.set(node, Math.min(low.get(node), low.get(next))); }
        else if (stacked.has(next)) low.set(node, Math.min(low.get(node), indices.get(next)));
    }
    if (low.get(node) !== indices.get(node)) return;
    const component = [];
    while (stack.length) {
        const member = stack.pop(); stacked.delete(member); component.push(member);
        if (member === node) break;
    }
    components.push(component);
}
for (const file of files) if (!indices.has(file)) visit(file);

const cycles = components.filter(component => component.length > 1).map(component =>
    component.map(file => path.relative(root, file)).sort());
assert.deepEqual(cycles, [], "the shipped static ESM graph must remain acyclic");

const selection = await readFile(path.join(root, "session/selection.js"), "utf8");
for (const dependency of ["../view/", "../input/", "../net/", "../core/state"]) {
    assert.ok(!selection.includes(dependency), "selection policy must not import " + dependency);
}

const used = new Set();
for (const file of files) {
    const source = await readFile(file, "utf8");
    for (const match of source.matchAll(/callSessionUI\("([^"]+)"/g)) used.add(match[1]);
    for (const match of source.matchAll(/callSessionUI\.apply\(null, \["([^"]+)"/g)) used.add(match[1]);
}
const main = await readFile(path.join(root, "main.js"), "utf8");
const composition = main.match(/bindSessionUI\(\{([\s\S]*?)\n\}\);/);
assert.ok(composition, "main.js remains the explicit session UI composition root");
const bound = new Set([...composition[1].matchAll(/^\s{4}([A-Za-z][A-Za-z0-9]*):/gm)]
    .map(match => match[1]));
assert.deepEqual([...used].filter(name => !bound.has(name)), [],
    "every static session UI callback is bound at the composition root");

assert.throws(() => sessionUICallback("missing"), /not bound/,
    "an unbound callback fails closed instead of silently becoming a no-op");
assert.throws(() => bindSessionUI({ broken: true }), /must be a function/,
    "the binding boundary refuses a non-callable implementation");
let applied = null;
bindSessionUI({ applied: function () { applied = [...arguments]; return "ok"; } });
assert.equal(callSessionUI.apply(null, ["applied", "one", "two"]), "ok",
    "the apply-form callback boundary reaches the registered implementation");
assert.deepEqual(applied, ["one", "two"], "apply forwards every callback argument");
assert.throws(() => callSessionUI("missing"), /not bound/,
    "replacing bindings leaves no stale hidden callback authority");

if (process.argv.includes("--mutate-missing-callback")) {
    const uiSource = await readFile(path.join(root, "session/ui.js"), "utf8");
    const mutatedSource = uiSource.replace(
        'throw new Error("session UI callback " + name + " is not bound");',
        "return function () {};");
    assert.notEqual(mutatedSource, uiSource, "missing-callback mutation target remains present");
    const mutated = await import("data:text/javascript;base64," +
        Buffer.from(mutatedSource).toString("base64"));
    assert.throws(() => mutated.callSessionUI("unbound"), /not bound/,
        "a missing production callback must fail closed");
}

console.log(`web module boundaries: ${files.length} modules, 0 cyclic SCCs, ${used.size} callbacks bound`);
