/** A long background activity stays inside the Chat footer on a phone and a desktop. */
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
const built=await build({entryPoints:[fileURLToPath(new URL('../src/project-assistants/motion-surfaces.fixture.tsx',import.meta.url))],bundle:true,write:false,outfile:'motion.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"','import.meta.env':'{}'},external:['/hero-orchestration-v4-task-clinic.webp']})
const files=new Map(built.outputFiles.map(f=>['/'+path.basename(f.path),f.contents]))
const server=createServer((req,res)=>{
 const name=new URL(req.url,'http://fixture').pathname
 if(files.has(name)){res.setHeader('Content-Type',name.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(name));return}
 if(name.startsWith('/v1/')){res.writeHead(500);res.end('No real API in this fixture');return}
 res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#080808;color:#fafafa;font:15px system-ui}.pane-list{height:80vh}footer{position:fixed;bottom:0;padding:12px}</style><link rel="stylesheet" href="/motion.css"><div id="app"></div><script type="module" src="/motion.js"></script>')
})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const origin=`http://127.0.0.1:${server.address().port}`
// The shape observed on a phone: a raw shell command with the person's home directory.
const command='cd /Users/example/code/clawdline; sed -n 1,200p web/console/src/project-assistants/BackgroundTasks.tsx && rg -n "pa-background" web/console/src | head -80'
// A long activity that is not a shell line keeps its words and must be cut by layout alone.
const longRead='Read web/console/src/project-assistants/background-tasks.css and web/console/src/project-assistants/BackgroundTasks.tsx together'
const cases=[['command',command],['read',longRead]]
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href),results=[]
try{for(const engine of ['chromium','webkit']){
 const browser=await pw[engine].launch({headless:true})
 try{for(const [language,width,height] of [['zh-Hant',390,844],['en',1280,800]])for(const [kind,activity] of cases){
  const page=await browser.newPage({viewport:{width,height}}),errors=[]
  page.on('pageerror',e=>errors.push(String(e)))
  await page.route('**/v1/**',route=>route.abort())
  await page.goto(origin+'/?lang='+language)
  await page.locator('.pa-dock').waitFor()
  await page.evaluate(()=>window.fixtureMotion.phase('idle'))
  await page.locator('.pa-dock').click()
  await page.evaluate(activity=>window.fixtureMotion.background('active',activity),activity)
  await page.locator(`.pa-bot-row[data-bot-key='${JSON.stringify(['motion','bot-0'])}']`).click()
  const capsule=page.locator('.pa-background-capsule')
  await capsule.getByText('Check the notes',{exact:true}).waitFor({state:'attached'})
  await page.locator('.pa-chat-metadata').waitFor()
  const box=await page.evaluate(()=>{
   const rect=selector=>{const r=document.querySelector(selector).getBoundingClientRect();return {left:r.left,right:r.right,top:r.top,bottom:r.bottom}}
   const capsule=document.querySelector('.pa-background-capsule')
   const inside=[...capsule.querySelectorAll('*')].map(node=>node.getBoundingClientRect()).filter(r=>r.width>0)
   return {capsule:rect('.pa-background-capsule'),footer:rect('.pa-chat-footer'),metadata:rect('.pa-chat-metadata'),
    childRight:Math.max(...inside.map(r=>r.right)),childBottom:Math.max(...inside.map(r=>r.bottom)),
    footerInner:(()=>{const f=document.querySelector('.pa-chat-footer'),s=getComputedStyle(f),r=f.getBoundingClientRect();return r.right-parseFloat(s.paddingRight)})(),
    text:capsule.textContent,viewport:innerWidth,scroll:document.documentElement.scrollWidth}
  })
  await page.screenshot({path:path.join(artifacts,`${label}-${engine}-${language}-${width}x${height}-${kind}.png`)})
  assert.ok(box.capsule.right<=box.footerInner+0.5,`${engine} ${width} ${kind}: capsule right ${box.capsule.right} exceeds footer content ${box.footerInner}`)
  assert.ok(box.childRight<=box.capsule.right+0.5,`${engine} ${width} ${kind}: capsule content right ${box.childRight} exceeds capsule ${box.capsule.right}`)
  assert.ok(box.childBottom<=box.metadata.top+0.5&&box.capsule.bottom<=box.metadata.top+0.5,`${engine} ${width} ${kind}: capsule bottom ${Math.max(box.childBottom,box.capsule.bottom)} overlaps metadata top ${box.metadata.top}`)
  assert.ok(box.scroll<=box.viewport,`${engine} ${width} ${kind}: page scrolls sideways`)
  assert.ok(!box.text.includes('/Users/'),`${engine} ${width} ${kind}: the capsule shows the home directory: ${box.text}`)
  for(const kept of [language==='en'?'Background':'背景','Check the notes'])assert.ok(box.text.includes(kept),`${engine} ${width} ${kind}: lost ${kept}`)
  assert.ok(await capsule.locator('.pa-background-title').isVisible(),`${engine} ${width} ${kind}: the title has no room`)
  assert.equal(await capsule.locator('.pa-background-mark').count(),1)
  assert.equal(await capsule.locator('time').count(),1)
  assert.ok(await capsule.locator('time').isVisible())
  assert.ok(await capsule.locator('.pa-background-stop').isVisible())
  assert.deepEqual(errors,[])
  if(kind==='read')assert.ok(box.text.includes('Read web/console'),`${engine} ${width}: a plain activity keeps its words`)
  results.push({engine,language,width,height,kind,...box})
  console.log(engine,language,width,kind,'passed');await page.close()
 }}finally{await browser.close()}
}}finally{server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
await writeFile(path.join(artifacts,`${label}-capsule-evidence.json`),JSON.stringify(results,null,2)+'\n')
