#!/usr/bin/env node
// Select local verification work. Integration and CI still execute every test.
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, extname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { changedPaths, classify } from './check-plan.mjs';

export function decodePackages(text) {
  const values = [];
  let start = 0, depth = 0, quoted = false, escaped = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) { if (escaped) escaped = false; else if (c === '\\') escaped = true; else if (c === '"') quoted = false; continue; }
    if (c === '"') quoted = true;
    else if (c === '{') { if (depth++ === 0) start = i; }
    else if (c === '}' && --depth === 0) values.push(JSON.parse(text.slice(start, i + 1)));
  }
  if (depth || quoted || !values.length) throw new Error('incomplete Go package inventory');
  return values;
}

export function selectGo(paths, packages, root) {
  if (packages.some(p => p.Error || p.Incomplete || !p.Dir || !p.ImportPath)) throw new Error('unreadable Go package inventory');
  const byName = new Map(packages.map(p => [p.ImportPath, p]));
  const changed = new Set(), production = new Set();
  const inputs = [...paths];
  if (paths.some(p => p.startsWith('api/v1/'))) inputs.push('internal/contract/zz_generated.go');
  for (const path of inputs.filter(p => /^(internal|cmd|skills)\//.test(p))) {
    const absolute = resolve(root, path);
    const owners = packages.filter(p => absolute.startsWith(resolve(p.Dir) + '/')).sort((a,b) => b.Dir.length-a.Dir.length);
    if (!owners.length) throw new Error(`no package owns ${path}`);
    changed.add(owners[0].ImportPath);
    if (!/_test\.go$/.test(path) && !path.includes('/testdata/')) production.add(owners[0].ImportPath);
  }
  const callers = new Set(production);
  let grew = true;
  while (grew) {
    grew = false;
    for (const p of packages) {
      const imports = [...(p.Imports ?? []), ...(p.TestImports ?? []), ...(p.XTestImports ?? [])];
      if (!callers.has(p.ImportPath) && imports.some(i => callers.has(i))) { callers.add(p.ImportPath); grew = true; }
    }
  }
  return {
    tests: [...changed].sort(),
    compile: [...callers].filter(p => !changed.has(p) && byName.has(p)).sort(),
  };
}

// Local modules use explicitly paired test files. Ambiguous inputs retain
// their entire package; CI still runs cross-file and transitive coverage.
export function selectGoRuns(paths, packages, root, selected) {
  return selected.map(name => {
    const pkg = packages.find(p => p.ImportPath === name);
    const changed = paths.filter(p => resolve(root,p).startsWith(resolve(pkg.Dir)+'/'));
    const inventory = [...(pkg.TestGoFiles ?? []), ...(pkg.XTestGoFiles ?? [])];
    const files = new Set();
    if (!changed.length || !inventory.length) return { package: name, pattern: '' };
    for (const path of changed) {
      if (!path.endsWith('.go')) return { package: name, pattern: '' };
      // Server construction belongs to the route module; its handlers retain
      // their own paired tests and CI retains the complete HTTP runtime suite.
      const filename = path === 'internal/transport/http/server.go' ? 'routes_test.go' : path.slice(path.lastIndexOf('/')+1).replace(/(?:_test)?\.go$/, '_test.go');
      if (!inventory.includes(filename)) return { package: pkg.ImportPath, pattern: '' };
      files.add(filename);
    }
    const names = new Set();
    for (const file of files) {
      const body = readFileSync(join(pkg.Dir,file),'utf8');
      for (const match of body.matchAll(/^func\s+((?:Test|Fuzz)\w+|Example\w*)\s*\(/gm)) if (match[1] !== 'TestMain') names.add(match[1]);
    }
    return { package: pkg.ImportPath, pattern: names.size ? '^('+[...names].sort().join('|')+')$' : '' };
  });
}

export function guardPlan(paths, full) {
  const any = re => full || paths.some(p => re.test(p));
  return {
    format: any(/\.go$/),
    machine: any(/\.(go|ts|tsx)$|^(web\/console\/public\/catalogs\/|cmd\/clawdline\/cli_catalogs\/|internal\/productcopy\/).*\.json$/),
    capacity: any(/\.go$|^api\/v1\/|^web\/console\/(public\/catalogs\/|src\/next-strings\.ts$)/),
    // contract-gen also renders the capacity, route and refusal references in
    // skills/clawdline from these packages, and the Cloud decoders in
    // internal/app/cloudops and the console's carry lists from the vocabulary
    // table; a change to one of them can leave those files stale while api/ is
    // untouched.
    contract: any(/^api\/v1\/|^internal\/contract\/|^web\/contract\/|^tools\/contract-gen\/|^internal\/domain\/capacity\/|^internal\/domain\/work\/|^internal\/domain\/cloudvocab\/|^internal\/app\/cloudops\/zz_|^web\/console\/src\/cloud\/carry-generated\.ts$|^internal\/app\/orchestrator\/|^internal\/transport\/http\/|^skills\/clawdline\/(capacity|routes|refusals)/),
    // A new register row needs a measure in internal/transport/http; that
    // package's paired-test selection never reaches the two tests that say so
    // (2026-11-27: two rows reached main without one and only CI noticed).
    measures: any(/^internal\/domain\/capacity\/|^internal\/transport\/http\/(capacity_measures|testdata\/capacity_measures)/),
    legacy: any(/^web\/console\/(src\/legacy\/|public\/strings\/)/),
    borders: any(/^web\/.*\.css$/),
    shell: any(/\.sh$/),
    tooling: full,
  };
}

export function affectedTests(changes, imports, tests) {
  const affected = new Set(changes);
  let grew = true;
  while (grew) {
    grew = false;
    for (const [file, deps] of imports) if (!affected.has(file) && deps.some(d => affected.has(d))) { affected.add(file); grew = true; }
  }
  return tests.filter(t => affected.has(t)).sort();
}

function filesBelow(root, dir) {
  const files = [];
  for (const entry of readdirSync(join(root, dir), { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) { if (!['node_modules', 'dist', '.git'].includes(entry.name)) files.push(...filesBelow(root, path)); }
    else files.push(path);
  }
  return files;
}

export async function selectWeb(paths, root) {
  const { default: ts } = await import('../web/node_modules/typescript/lib/typescript.js');
  const files = ['web/console/src', 'web/console/tools', 'web/core/src', 'web/contract/src'].flatMap(p => filesBelow(root, p));
  const tests = files.filter(p => p.startsWith('web/console/') && /\.test\.(ts|mjs)$/.test(p));
  // Catalogs, configuration and fixtures can be read without an import. Keep
  // their runtime coverage broad rather than guess about those dependencies.
  if (paths.some(p => p.startsWith('web/') && !/^web\/console\/public\/(catalogs|strings|view-plugins)\//.test(p) && !['web/console/semantic-risk-keys.json','web/console/semantic-screen-groups.json'].includes(p) && (!/\.(ts|tsx|js|mjs|css)$/.test(p) || !files.includes(p)))) return { tests, broad: true };
  const configFile = ts.readConfigFile(join(root, 'web/console/tsconfig.json'), ts.sys.readFile);
  if (configFile.error) throw new Error('unreadable TypeScript configuration');
  const config = ts.parseJsonConfigFileContent(configFile.config, ts.sys, join(root, 'web/console'));
  if (config.errors.length) throw new Error('invalid TypeScript configuration');
  const imports = new Map();
  let dynamic = false;
  for (const file of files.filter(p => /\.(ts|tsx|js|mjs)$/.test(p))) {
    const absolute = join(root, file), body = readFileSync(absolute, 'utf8');
    const source = ts.createSourceFile(absolute, body, ts.ScriptTarget.Latest, true);
    const deps = [];
    const add = spec => {
      let resolved = ts.resolveModuleName(spec, absolute, config.options, ts.sys).resolvedModule?.resolvedFileName;
      if (!resolved && spec.startsWith('.')) { const candidate = resolve(dirname(absolute), spec); if (existsSync(candidate)) resolved = candidate; }
      if (resolved && !resolved.includes('/node_modules/')) deps.push(relative(root, resolved));
    };
    const visit = node => {
      if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier && ts.isStringLiteralLike(node.moduleSpecifier)) {
        const clause = ts.isImportDeclaration(node) ? node.importClause : node;
        const bindings = ts.isImportDeclaration(node) ? node.importClause?.namedBindings : node.exportClause;
        const onlyTypes = clause?.isTypeOnly || bindings && 'elements' in bindings && bindings.elements.length > 0 && bindings.elements.every(e => e.isTypeOnly) && !(ts.isImportDeclaration(node) && node.importClause?.name);
        if (!onlyTypes) add(node.moduleSpecifier.text);
      }
      if (ts.isCallExpression(node) && (node.expression.kind === ts.SyntaxKind.ImportKeyword || ts.isIdentifier(node.expression) && node.expression.text === 'require')) {
        if (node.arguments.length === 1 && ts.isStringLiteralLike(node.arguments[0])) add(node.arguments[0].text);
        else { dynamic = true; deps.push(...paths); }
      }
      if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression) && node.expression.name.text === 'glob') { dynamic = true; deps.push(...paths); }
      // Filesystem reads in tests may reference production text or fixtures
      // outside the module graph. Such readers always remain in the local set.
      ts.forEachChild(node, visit);
    };
    visit(source);
    imports.set(file, deps);
    if (/\.test\.(ts|mjs)$/.test(file) && /\b(readFile|readFileSync|readdir|readdirSync|globSync)\b/.test(body)) imports.get(file).push(...paths);
  }
  return { tests: affectedTests(paths, imports, tests), broad: dynamic };
}

async function plan(full) {
  const root = process.cwd();
  const { paths, known } = changedPaths();
  const base = known && !paths.length && !full ? { go: false, web: false } : classify(paths, full, known);
  const broad = full || !known || paths.some(p => !/^(web\/|docs\/|plugins\/|README(?:\.[\w-]+)?\.md$|CHANGELOG\.md$|internal\/|cmd\/|skills\/|api\/v1\/)/.test(p));
  const result = { paths, full: broad, reason: broad ? 'full integration or unclassified infrastructure inputs' : 'changed-package tests; caller and full runtime coverage stay in integration/CI', go: { tests: [], compile: [], runs: [] }, guards: guardPlan(paths,broad), web: base.web, webTests: [], webBroad: false };
  if (broad) { result.web = true; return result; }
  try {
    if (base.go) {
      const platformInputs = paths.filter(p => p.endsWith('.go') && (/_(windows|darwin|linux|unix|other)(?:_test)?\.go$/.test(p) || existsSync(p) && /^\/\/go:build /m.test(readFileSync(p, 'utf8'))));
      if (platformInputs.length) { result.platformInputs = platformInputs; result.reason += '; foreign-platform verification remains in integration/CI'; }
      const listed = spawnSync('go', ['list', '-json=Dir,ImportPath,Imports,TestImports,XTestImports,TestGoFiles,XTestGoFiles,Error,Incomplete', './...'], { encoding: 'utf8' });
      if (listed.status !== 0) throw new Error('Go package discovery failed');
      const packages = decodePackages(listed.stdout);
      result.go = selectGo(paths, packages, root);
      result.go.runs = selectGoRuns(paths, packages, root, result.go.tests);
      if (!result.go.tests.length) throw new Error('no changed Go package could be selected');
    }
    if (base.web) { const web = await selectWeb(paths, root); result.webTests = web.tests; result.webBroad = web.broad; }
  } catch (error) { result.full = true; result.web = true; result.guards = guardPlan(paths,true); result.reason = `conservative fallback: ${error.message}`; }
  return result;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  if (args[0] === '--field') {
    const data = JSON.parse(readFileSync(args[1], 'utf8'));
    let value = data;
    for (const part of args[2].split('.')) value = value[part];
    if (args.includes('--nul')) process.stdout.write((args[2] === 'go.runs' ? value.flatMap(v => [v.package,v.pattern]) : value.map(v => args[2] === 'paths' ? `:(literal)${v}` : v)).join('\0') + (value.length ? '\0' : ''));
    else console.log(typeof value === 'string' ? value : JSON.stringify(value));
  } else {
    const result = await plan(args.includes('--full'));
    if (args.includes('--web')) { result.web = true; result.webMode = 'full web checks requested'; }
    if (args[0] === '--out') writeFileSync(args[1], JSON.stringify(result, null, 2) + '\n');
    else console.log(JSON.stringify(result, null, 2));
  }
}
