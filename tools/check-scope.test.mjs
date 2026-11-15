import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { affectedTests, decodePackages, selectGo, selectWeb, selectGoRuns, guardPlan } from './check-scope.mjs';

test('Go changes run their own tests and compile transitive callers including test-only imports', () => {
  const packages = [
    { Dir: '/repo/internal/leaf', ImportPath: 'm/leaf' },
    { Dir: '/repo/internal/middle', ImportPath: 'm/middle', Imports: ['m/leaf'] },
    { Dir: '/repo/cmd/app', ImportPath: 'm/app', TestImports: ['m/middle'] },
    { Dir: '/repo/internal/unrelated', ImportPath: 'm/unrelated' },
  ];
  assert.deepEqual(selectGo(['internal/leaf/a.go'], packages, '/repo'), { tests: ['m/leaf'], compile: ['m/app', 'm/middle'] });
  assert.deepEqual(selectGo(['internal/leaf/a_test.go'], packages, '/repo'), { tests: ['m/leaf'], compile: [] });
  assert.deepEqual(selectGo(['internal/leaf/testdata/fixture.json'], packages, '/repo'), { tests: ['m/leaf'], compile: [] });
  assert.throws(() => selectGo(['internal/deleted/a.go'], packages, '/repo'), /no package owns/);
  assert.throws(() => selectGo([], [{ Error: {} }], '/repo'), /unreadable/);
});

test('API schema and route changes select contracts and changed packages without full runtime suites', () => {
  const packages = [
    { Dir: '/repo/internal/contract', ImportPath: 'm/contract' },
    { Dir: '/repo/internal/http', ImportPath: 'm/http', Imports: ['m/contract'] },
    { Dir: '/repo/internal/unrelated', ImportPath: 'm/unrelated' },
  ];
  assert.deepEqual(selectGo(['api/v1/routes.json'], packages, '/repo'), { tests: ['m/contract'], compile: ['m/http'] });
  assert.deepEqual(selectGo(['api/v1/sessions.schema.json', 'internal/http/routes.go'], packages, '/repo'), { tests: ['m/contract', 'm/http'], compile: [] });
});

test('Go JSON inventory decoding handles quotes and concatenated records, rejecting truncation', () => {
  const data = [{ Dir: '/repo/a{b}', ImportPath: 'm/a', Doc: 'quote " }' }, { Dir: '/repo/b', ImportPath: 'm/b' }];
  assert.deepEqual(decodePackages(data.map(v => JSON.stringify(v)).join('\n')), data);
  for (const broken of ['', '{"Dir":', '{"Dir":"unterminated']) assert.throws(() => decodePackages(broken));
});

test('web dependency closure includes callers and modified tests without selecting unrelated tests', () => {
  const imports = new Map([['leaf.ts', []], ['middle.ts', ['leaf.ts']], ['related.test.ts', ['middle.ts']], ['other.test.ts', []]]);
  assert.deepEqual(affectedTests(['leaf.ts'], imports, ['related.test.ts', 'other.test.ts']), ['related.test.ts']);
  assert.deepEqual(affectedTests(['other.test.ts'], imports, ['related.test.ts', 'other.test.ts']), ['other.test.ts']);
});

test('real TypeScript resolution follows .js imports, keeps filesystem readers and falls back for fixtures', async () => {
  const root = mkdtempSync(join(tmpdir(), 'check-web-scope-'));
  const write = (p, text) => { mkdirSync(join(root, p, '..'), { recursive: true }); writeFileSync(join(root, p), text); };
  try {
    for (const dir of ['web/console/src', 'web/console/tools', 'web/core/src', 'web/contract/src']) mkdirSync(join(root, dir), { recursive: true });
    write('web/console/tsconfig.json', JSON.stringify({ compilerOptions: { allowJs: true, module: 'esnext', moduleResolution: 'bundler' }, include: ['src'] }));
    write('web/console/src/leaf.ts', 'export const leaf=1;');
    write('web/console/src/middle.ts', 'export {leaf} from "./leaf.js";');
    write('web/console/src/related.test.ts', 'import {leaf} from "./middle.js";');
    write('web/console/src/other.test.ts', 'export {};');
    write('web/console/tools/read.test.mjs', 'import {readFileSync} from "node:fs";');
    let result = await selectWeb(['web/console/src/leaf.ts'], root);
    assert.equal(result.broad, false);
    assert.deepEqual(result.tests, ['web/console/src/related.test.ts', 'web/console/tools/read.test.mjs']);
    result = await selectWeb(['web/console/public/view-plugins/fixture.zip'], root);
    assert.equal(result.broad, false);
    assert.deepEqual(result.tests, ['web/console/tools/read.test.mjs']);
    result = await selectWeb(['web/package-lock.json'], root);
    assert.equal(result.broad, true);
    assert.equal(result.tests.length, 3);
    write('web/console/src/dynamic.ts', 'export const load=(name:string)=>import(name);');
    result = await selectWeb(['web/console/src/leaf.ts'], root);
    assert.equal(result.broad, true);
    assert.equal(result.tests.length, 2);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test('paired Go tests retain whole-package fallback and guards follow their own inputs', () => {
  const root=mkdtempSync(join(tmpdir(),'check-go-module-'));
  try {
    mkdirSync(join(root,'internal/http'),{recursive:true});
    writeFileSync(join(root,'internal/http/voice_test.go'),'package http\nfunc TestVoice(t *testing.T) {}\nfunc TestVoiceFailure(t *testing.T) {}\n');
    const packages=[{Dir:join(root,'internal/http'),ImportPath:'m/http',TestGoFiles:['voice_test.go']}];
    assert.deepEqual(selectGoRuns(['internal/http/voice.go'],packages,root,['m/http']),[{package:'m/http',pattern:'^(TestVoice|TestVoiceFailure)$'}]);
    assert.equal(selectGoRuns(['internal/http/shared.go'],packages,root,['m/http'])[0].pattern,'');
    assert.equal(selectGoRuns(['internal/http/testdata/voice.json'],packages,root,['m/http'])[0].pattern,'');
    const front=guardPlan(['web/console/src/overlays/info.ts'],false);
    for(const key of ['format','capacity','contract','legacy','borders','shell','tooling']) assert.equal(front[key],false,key);
    assert.equal(guardPlan(['api/v1/routes.json'],false).contract,true);
    assert.equal(guardPlan(['internal/http/voice.go'],false).capacity,true);
    assert.equal(guardPlan(['web/console/src/page.css'],false).borders,true);
    assert.equal(Object.values(guardPlan([],true)).every(Boolean),true);
  } finally {rmSync(root,{recursive:true,force:true});}
});

test('runtime scope excludes type-only imports and generated validation inventories stay selective', async () => {
 const root=mkdtempSync(join(tmpdir(),'check-runtime-types-'));
 const write=(p,s)=>{mkdirSync(join(root,p,'..'),{recursive:true});writeFileSync(join(root,p),s)};
 try {
  write('web/console/tsconfig.json',JSON.stringify({compilerOptions:{module:'ESNext',moduleResolution:'Bundler'},include:['src']}));
  write('web/console/src/types.ts','export type Model = {id: string}; export const value=1;');
  write('web/console/src/types.test.ts',"import type { Model } from './types.js'; export {};");
  write('web/console/src/named-types.test.ts',"import { type Model } from './types.js'; export {};");
  write('web/console/src/runtime.test.ts',"import { value } from './types.js'; console.log(value);");
  write('web/console/src/reader.test.mjs',"import {readFileSync} from 'node:fs'; readFileSync('semantic-risk-keys.json');");
  write('web/console/tools/fixture.mjs','export {};');write('web/core/src/index.ts','export {};');write('web/contract/src/index.ts','export {};');
  let got=await selectWeb(['web/console/src/types.ts'],root);
  assert.deepEqual(got.tests,['web/console/src/reader.test.mjs','web/console/src/runtime.test.ts']);
  got=await selectWeb(['web/console/semantic-risk-keys.json'],root);
  assert.deepEqual(got.tests,['web/console/src/reader.test.mjs']);
  const pkg={Dir:join(root,'internal/transport/http'),ImportPath:'m/http',TestGoFiles:['routes_test.go']};
  write('internal/transport/http/routes_test.go','package http\nfunc TestRouteTable(t *testing.T) {}\n');
  assert.deepEqual(selectGoRuns(['internal/transport/http/server.go'],[pkg],root,['m/http']),[{package:'m/http',pattern:'^(TestRouteTable)$'}]);
 }finally{rmSync(root,{recursive:true,force:true});}
});

// On 2026-10-17 a merge changed internal/domain/capacity only, contract-gen's
// capacity reference went stale, and the local gate skipped contract-gen.
test('a capacity, refusal-source or reference change selects contract-gen', () => {
  for (const path of ['internal/domain/capacity/capacity.go', 'internal/domain/work/v2.go',
    'internal/app/orchestrator/dispatch.go', 'internal/transport/http/server.go', 'skills/clawdline/capacity.md',
    'internal/domain/cloudvocab/vocabulary.json', 'internal/app/cloudops/zz_decode_generated.go',
    'web/console/src/cloud/carry-generated.ts']) {
    assert.equal(guardPlan([path], false).contract, true, path)
  }
  assert.equal(guardPlan(['internal/adapters/store/sqlite.go'], false).contract, false)
  assert.equal(guardPlan(['internal/app/cloudops/ops.go'], false).contract, false)
  assert.equal(guardPlan(['web/console/src/cloud/carry.ts'], false).contract, false)
})
