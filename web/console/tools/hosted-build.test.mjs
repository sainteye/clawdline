import assert from "node:assert/strict"
import { test } from "node:test"
import { fileURLToPath } from "node:url"
import { resolveConfig } from "vite"
import { productionCloud, validateHostedDeclaration } from "./hosted-declaration.mjs"
import { validateHostedBundle } from "./hosted-bundle.mjs"

const configFile = fileURLToPath(new URL("../vite.config.ts", import.meta.url))

test("the actual Vite build refuses the incident's truthy non-object declaration", async () => {
  const previous = process.env.VITE_HOSTED_CONSOLE
  process.env.VITE_HOSTED_CONSOLE = "1"
  try {
    await assert.rejects(resolveConfig({ configFile, mode: "production" }, "build"), /VITE_HOSTED_CONSOLE/)
  } finally {
    if (previous === undefined) delete process.env.VITE_HOSTED_CONSOLE
    else process.env.VITE_HOSTED_CONSOLE = previous
  }
})

test("local builds remain local while malformed hosted declarations fail closed", () => {
  assert.equal(validateHostedDeclaration(undefined), null)
  assert.equal(validateHostedDeclaration(""), null)
  for (const raw of ["1", "true", "null", "[]", "{}", "not JSON",
    JSON.stringify({ ...productionCloud, relay_url: "https://relay.clawdline.com/v1/connect" }),
    JSON.stringify({ ...productionCloud, api_origin: "http://api.clawdline.com" }),
    JSON.stringify({ ...productionCloud, v: 2 })]) {
    assert.throws(() => validateHostedDeclaration(raw), /VITE_HOSTED_CONSOLE/)
  }
  assert.deepEqual(validateHostedDeclaration(JSON.stringify(productionCloud)), {
    appOrigin: productionCloud.app_origin, apiOrigin: productionCloud.api_origin,
    relayURL: productionCloud.relay_url, build: "", strings: {},
  })
})

test("the served bundle check refuses CloudGate with the incident's invalid declaration", () => {
  assert.throws(() => validateHostedBundle('const declared="1";import("./CloudGate.js")'), /no usable production/)
  assert.throws(() => validateHostedBundle('import("./CloudGate.js")'), /no usable production/)
  const valid = `const declared=${JSON.stringify(JSON.stringify(productionCloud))};import("./CloudGate.js")`
  assert.equal(validateHostedBundle(valid), true)
  const stamp = "a".repeat(40)
  assert.throws(() => validateHostedBundle(valid, stamp), /no usable production/)
  const stamped = `const declared=${JSON.stringify(JSON.stringify({ ...productionCloud, build: stamp }))}`
  assert.equal(validateHostedBundle(stamped, stamp), true)
  assert.throws(() => validateHostedBundle(stamped, "b".repeat(40)), /no usable production/)
  assert.throws(() => validateHostedBundle(valid + "))"), /not valid JavaScript/)
  const wrong = { ...productionCloud, api_origin: "https://example.com" }
  assert.throws(() => validateHostedBundle(`const declared=${JSON.stringify(JSON.stringify(wrong))}`), /no usable production/)
})

// Publication checks must reject a successful HTTP response containing the SPA fallback.
import {stylesheetPaths,verifyStylesheet} from './check-hosted-assets.mjs'
test('hosted stylesheet discovery follows generated links and rejects foreign assets',()=>{
 assert.deepEqual(stylesheetPaths(`<!-- <link rel="stylesheet" href="/assets/unused.css"> --><link href='/assets/main.css' rel='stylesheet'><link REL="stylesheet" HREF="/assets/tokens.css">`,'https://app.clawdline.com'),['/assets/main.css','/assets/tokens.css'])
 assert.throws(()=>stylesheetPaths('<link rel="stylesheet" href="https://elsewhere.invalid/main.css">','https://app.clawdline.com'),/unexpected_location/)
 assert.throws(()=>stylesheetPaths('<script src="/assets/main.js"></script>','https://app.clawdline.com'),/stylesheet_missing/)
})
test('hosted stylesheet verification rejects HTML fallback, missing MIME and empty assets',()=>{
 verifyStylesheet('/assets/main.css','HTTP/2 200\r\ncontent-type: text/css; charset=utf-8\r\n','.call { display: flex }')
 assert.throws(()=>verifyStylesheet('/assets/main.css','content-type: text/html','<!-- No doctype -->'),/wrong_type/)
 assert.throws(()=>verifyStylesheet('/assets/main.css','content-type: text/css','<!doctype html><main>Loading</main>'),/invalid_body/)
 assert.throws(()=>verifyStylesheet('/assets/main.css','','.call{}'),/wrong_type/)
 assert.throws(()=>verifyStylesheet('/assets/main.css','content-type: text/css','  '),/invalid_body/)
})


test('identical hosted styles use a different asset URL in a new source deployment', async () => {
 const {mkdtempSync,writeFileSync,rmSync,realpathSync}=await import('node:fs')
 const {tmpdir}=await import('node:os')
 const {join}=await import('node:path')
 const {build}=await import('vite')
 const {hostedAssetPattern}=await import('./check-hosted-assets.mjs')
 const root=realpathSync(mkdtempSync(join(tmpdir(),'hosted-style-deployment-')))
 try {
  writeFileSync(join(root,'index.html'),'<link rel="stylesheet" href="/main.css"><main>Preview</main>')
  writeFileSync(join(root,'main.css'),'.call{display:flex}')
  const names=[]
  for(const stamp of ['a'.repeat(40),'b'.repeat(40)]){
   const result=await build({root,configFile:false,logLevel:'silent',build:{write:false,rollupOptions:{output:{assetFileNames:hostedAssetPattern(stamp)}}}})
   const outputs=Array.isArray(result)?result:[result]
   const assets=outputs.flatMap(item=>item.output).filter(item=>item.type==='asset'&&item.fileName.endsWith('.css'))
   assert.equal(assets.length,1)
   names.push(assets[0].fileName)
  }
  assert.notEqual(names[0],names[1])
 }finally{rmSync(root,{recursive:true,force:true})}
})
