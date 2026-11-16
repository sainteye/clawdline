/** Render the real line avatars and picker in installed browsers; no daemon or model. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath,pathToFileURL} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'

const artifacts=process.env.BOTS_ARTIFACTS
if(!artifacts||!process.env.PLAYWRIGHT_MODULE)throw Error('Set BOTS_ARTIFACTS and the installed PLAYWRIGHT_MODULE')
await mkdir(artifacts,{recursive:true})
const built=await build({stdin:{contents:`
import React,{useState} from 'react';import {createRoot} from 'react-dom/client';
import {BotAvatar,AppearancePicker} from './BotAvatar';import {palette} from './avatar-paths';import {words} from './words';import './styles.css';
function App(){const [appearance,setAppearance]=useState({shape:'blob',color:palette[0]});const w=words(new URL(location.href).searchParams.get('lang')||'en');
return <main className="project-assistants"><h1>Bots</h1><AppearancePicker value={appearance} onChange={setAppearance} w={w}/>
<output>{JSON.stringify(appearance)}</output><section className="states">{['idle','working','needsInput'].map(mood=><div key={mood} data-state={mood}><BotAvatar appearance={appearance} mood={mood} size={76}/><p>{mood}</p></div>)}</section>
<section className="sizes">{[20,30,44].map(size=><BotAvatar key={size} appearance={appearance} size={size}/>)}</section></main>}
createRoot(document.querySelector('#app')).render(<App/>);`,resolveDir:fileURLToPath(new URL('../src/project-assistants',import.meta.url)),loader:'tsx'},bundle:true,write:false,outfile:'avatars.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'}})
const files=new Map(built.outputFiles.map(f=>['/'+path.basename(f.path),f.contents]))
const server=createServer((req,res)=>{
 const pathname=new URL(req.url,'http://fixture').pathname
 if(files.has(pathname)){res.setHeader('Content-Type',pathname.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(pathname));return}
 res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#080808;color:#fafafa;font:15px system-ui}main{max-width:460px;margin:24px auto;padding:24px}section{display:flex;justify-content:space-around;align-items:center;margin:40px 0}.states>div{text-align:center}.states p{margin-top:16px}output{display:block;font:11px monospace;overflow-wrap:anywhere}</style><link rel="stylesheet" href="/avatars.css"><div id="app"></div><script type="module" src="/avatars.js"></script>')
})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const origin=`http://127.0.0.1:${server.address().port}`
console.log(origin)
if(process.argv.includes('--serve'))await new Promise(()=>{})
else{
 const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href),evidence=[]
 try{
  for(const engine of ['chromium','webkit']){
   const browser=await pw[engine].launch({headless:true})
   try{for(const [language,width,height] of [['en',1280,900],['zh-Hant',390,844]]){
    const page=await browser.newPage({viewport:{width,height}}),errors=[]
    page.on('pageerror',e=>errors.push(String(e)))
    await page.goto(origin+'/?lang='+language)
    const idle=page.locator('[data-state=idle] svg'),working=page.locator('[data-state=working] svg'),waiting=page.locator('[data-state=needsInput] svg')
    await idle.waitFor()
    const still=await idle.screenshot();await page.waitForTimeout(170)
    assert.ok(still.equals(await idle.screenshot()),'Idle gradient and glow stay still')
    await idle.locator('path[filter]').evaluate(el=>el.removeAttribute('filter'))
    assert.ok(!still.equals(await idle.screenshot()),'The static neon halo is visible')
    await page.reload();await idle.waitFor()
    const first=await working.screenshot();await page.waitForTimeout(200)
    assert.ok(!first.equals(await working.screenshot()),'Working light moves visibly')
    const wave=waiting.locator('path').first(),phase=await wave.getAttribute('transform')
    await page.waitForTimeout(200);assert.notEqual(await wave.getAttribute('transform'),phase,'Waiting contour expands')
    await page.emulateMedia({reducedMotion:'reduce'});await page.waitForTimeout(100)
    const reduced=await working.screenshot();await page.waitForTimeout(200)
    assert.ok(reduced.equals(await working.screenshot()),'Reduced motion keeps a stable lit avatar')
    assert.equal(await wave.getAttribute('opacity'),'0')
    assert.equal(await working.locator('linearGradient').getAttribute('gradientTransform'),null)
    await page.locator('.pa-shapes button').nth(6).click()
    for(const button of await page.locator('.pa-colors button').all()){
     await button.click()
     const selected=JSON.parse(await page.locator('output').innerText())
     assert.equal(selected.shape,'cloud');assert.equal(selected.color,await button.getAttribute('aria-label'))
     assert.equal(await button.getAttribute('aria-pressed'),'true')
    }
    const ids=await page.locator('svg defs [id]').evaluateAll(els=>els.map(el=>el.id))
    assert.equal(new Set(ids).size,ids.length,'Repeated picker and roster previews have independent paints')
    for(const button of await page.locator('.pa-shapes button').all()){
     await button.click();assert.equal(await button.getAttribute('aria-pressed'),'true')
    }
    await page.screenshot({path:path.join(artifacts,`${engine}-${language}-neon-picker.png`),fullPage:true})
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
    assert.deepEqual(errors,[])
    evidence.push({engine,language,width,height,idleHalo:true,workingMotion:true,waitingWave:true,reducedMotion:true,paletteChoices:8,shapeChoices:8,uniquePaints:true,pageErrors:errors.length})
    await page.close()
   }}finally{await browser.close()}
  }
  await writeFile(path.join(artifacts,'avatar-evidence.json'),JSON.stringify({modelRequests:0,evidence},null,2)+'\n')
  console.log(JSON.stringify(evidence))
 }finally{server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
}
