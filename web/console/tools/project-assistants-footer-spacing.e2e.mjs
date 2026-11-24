/** The dispatched-work capsule scrolls with the conversation while a Chat loads and after it: it never sits on the composer, and the model row does not push it. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath,pathToFileURL} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
const artifacts=process.env.BOTS_ARTIFACTS
if(!artifacts||!process.env.PLAYWRIGHT_MODULE)throw Error('Set BOTS_ARTIFACTS and installed PLAYWRIGHT_MODULE')
await mkdir(artifacts,{recursive:true})
const label=process.env.BOTS_SHOT_LABEL||'after'
const built=await build({entryPoints:[fileURLToPath(new URL('../src/project-assistants/footer-spacing.fixture.tsx',import.meta.url))],bundle:true,write:false,outfile:'footer.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"','import.meta.env':'{}'},external:['/hero-orchestration-v4-task-clinic.webp']})
const files=new Map(built.outputFiles.map(f=>['/'+path.basename(f.path),f.contents]))
const server=createServer((req,res)=>{
 const name=new URL(req.url,'http://fixture').pathname
 if(files.has(name)){res.setHeader('Content-Type',name.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(name));return}
 if(name.startsWith('/v1/')){res.writeHead(500);res.end('No real API in this fixture');return}
 res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#080808;color:#fafafa;font:15px system-ui}</style><link rel="stylesheet" href="/footer.css"><div id="app"></div><script type="module" src="/footer.js"></script>')
})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const measure=page=>page.evaluate(()=>{
 const strip=document.querySelector('.pa-dispatched').getBoundingClientRect(),composer=document.querySelector('.pa-composer').getBoundingClientRect()
 const metadata=document.querySelector('.pa-chat-metadata')
 return {stripBottom:strip.bottom,composerTop:composer.top,gap:composer.top-strip.bottom,metadata:!!metadata,
  // The gap the capsule itself keeps: to the model row when it shows, else to the composer.
  below:(metadata?metadata.getBoundingClientRect().top:composer.top)-strip.bottom,
  inScroll:document.querySelector('.pa-chat-scroll').contains(document.querySelector('.pa-dispatched')),
  footer:document.querySelector('.pa-chat-footer').getBoundingClientRect().height}
})
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href),results=[],failures=[]
try{for(const engine of ['chromium','webkit']){
 const browser=await pw[engine].launch({headless:true})
 try{for(const [language,width,height] of [['zh-Hant',390,844],['en',1280,800]]){
  const page=await browser.newPage({viewport:{width,height}}),errors=[],name=`${engine} ${width}x${height}`
  page.on('pageerror',e=>errors.push(String(e)))
  await page.route('**/v1/**',route=>route.abort())
  await page.goto(origin+'/?lang='+language)
  await page.locator('.pa-dispatched').waitFor()
  await page.locator('.pa-composer').waitFor()
  assert.equal(await page.locator('.pa-chat-metadata').count(),0,`${name}: the model row showed before the first load`)
  const loading=await measure(page)
  await page.screenshot({path:path.join(artifacts,`${label}-${engine}-${width}x${height}-loading.png`)})
  await page.evaluate(()=>window.fixtureFooter.release())
  await page.locator('.pa-chat-metadata').waitFor()
  await page.waitForTimeout(400)
  const ready=await measure(page)
  await page.screenshot({path:path.join(artifacts,`${label}-${engine}-${width}x${height}-loaded.png`)})
  results.push({engine,language,width,height,loading,ready})
  console.log(name,'loading gap',loading.gap,'below',loading.below,'| loaded gap',ready.gap,'below',ready.below)
  const check=(ok,message)=>{if(!ok)failures.push(`${name}: ${message}`)}
  check(loading.gap>=8,`while loading, the capsule is ${loading.gap}px above the composer`)
  check(ready.below>=8,`after loading, the capsule is ${ready.below}px above the model row`)
  check(loading.inScroll&&ready.inScroll,`the capsule is outside the scroll area (loading ${loading.inScroll}, loaded ${ready.inScroll})`)
  check(errors.length===0,`page errors ${errors.join('; ')}`)
  await page.close()
 }}finally{await browser.close()}
}}finally{server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
await writeFile(path.join(artifacts,`${label}-footer-spacing-evidence.json`),JSON.stringify(results,null,2)+'\n')
assert.deepEqual(failures,[])
