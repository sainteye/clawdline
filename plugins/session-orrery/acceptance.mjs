/** Candidate-only acceptance through the production installer, store, frame, bridge and SDK. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {readFile,mkdir,writeFile} from "node:fs/promises"
import {createRequire} from "node:module"
import {fileURLToPath,pathToFileURL} from "node:url"
import {resolve} from "node:path"
const require=createRequire(new URL("../../web/package.json",import.meta.url))
const {build}=require("esbuild")
const root=fileURLToPath(new URL("../../",import.meta.url))
const artifactPath=process.env.PLUGIN_ARTIFACTS
if(!artifactPath)throw Error("Set PLUGIN_ARTIFACTS to a task-owned evidence directory")
if(!process.env.PLAYWRIGHT_MODULE)throw Error("Set PLAYWRIGHT_MODULE to an already installed local module")
const artifacts=resolve(artifactPath)
await mkdir(artifacts,{recursive:true})
const result=await build({stdin:{contents:`
import React from "react";import{createRoot}from"react-dom/client";
import{useViewPlugins}from"./view-plugins/Host";
import{setDockAvailable}from"./project-assistants/BotOrb";setDockAvailable(true);
const states=["waiting_you","waiting_session","holding","working","milestone_complete","work_complete","ready","unknown"];
const titles=["Approve the release notes","Review another Session","Hold for the next reading","Shape the new workspace","Navigation milestone","Package ready for review","A quiet workspace","Unclassified reading"];
const projects=[{key:"observatory",label:"Observatory · synthetic"},{key:"workshop",label:"Workshop · synthetic"},{key:"archive",label:"Archive · synthetic"}];
const rows=states.map((workState,i)=>({key:"synthetic-"+i,title:titles[i],project:projects[i%3],workState,freshness:"fresh"}));
rows.push({key:"hostile",title:"<img src=x onerror=alert(1)> & a very long Session title that must wrap safely across a narrow mobile screen",project:{key:"hostile-project",label:"<script>hostile label</script>"},workState:"working",freshness:"stale",connected:false},{key:"no-project",title:"A synthetic Session without a Project",project:null,workState:"unknown",freshness:"unknown"});
let listeners=new Set(),connected=true;
window.fixture={rows,navigations:[],change(){for(const fn of listeners)fn()},disconnect(){connected=false;this.change()},reconnect(){connected=true;this.change()},hidden(value){Object.defineProperty(document,"visibilityState",{configurable:true,get:()=>value?"hidden":"visible"});document.dispatchEvent(new Event("visibilitychange"))}};
const source={key:"synthetic-source",label:"Synthetic acceptance data",async read(){return{sessions:[...rows],connected}},subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn)},async prepareOpen(key){return()=>fixture.navigations.push(key)}};
function Host(){const api=useViewPlugins([source],document.documentElement.lang);return <><p>Synthetic acceptance data / 合成驗收資料</p><button id="manager" onClick={e=>api.openManager(e.currentTarget)}>Manage</button>{api.entry}{api.content}</>}
createRoot(document.getElementById("root")).render(<Host/>);
`,resolveDir:root+"web/console/src",loader:"tsx"},bundle:true,write:false,outfile:"host.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(result.outputFiles.map(f=>["/"+f.path.split("/").at(-1),f.text]))
const server=createServer((req,res)=>{const u=new URL(req.url,"http://test"),body=u.pathname==="/"?`<!doctype html><html lang="${u.searchParams.get("lang")==="zh-Hant"?"zh-Hant":"en"}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><style>body{margin:0}</style><div id="root"></div><script type="module" src="/host.js"></script>`:files.get(u.pathname);if(body===undefined){res.writeHead(404);res.end();return}res.setHeader("Content-Type",u.pathname.endsWith(".js")?"text/javascript":u.pathname.endsWith(".css")?"text/css":"text/html");res.end(body)})
await new Promise(r=>server.listen(0,"127.0.0.1",r))
const origin=`http://127.0.0.1:${server.address().port}`
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const reports=[]
try{for(const engine of ["chromium","webkit"])for(const width of [390,1440]){
 const lang=width===390?"zh-Hant":"en",zh=lang!=="en",browser=await pw[engine].launch({headless:true})
 const page=await browser.newPage({viewport:{width,height:1000},reducedMotion:"reduce"}),errors=[]
 page.on("pageerror",e=>errors.push(e.message))
 try{
  await page.goto(origin+"/?lang="+lang)
  await page.locator("#manager").click()
  await page.locator('input[type=file]:enabled').waitFor()
  await page.locator('input[type=file]:enabled').setInputFiles(root+"web/console/public/view-plugins/session-orrery.zip")
  await page.getByRole("button",{name:zh?"安裝套件":"Install package",exact:true}).click()
  await page.locator(".vp-installed").getByRole("button",{name:new RegExp(zh?"^開啟":"^Open")}).click()
  const f=page.frameLocator("iframe")
  await f.locator(".row").nth(9).waitFor()
  assert.equal(await f.locator(".planet").count(),10)
  assert.equal(await f.locator(".count").count(),8)
  assert.deepEqual(await f.locator("#counts strong").allTextContents(),["1","1","1","2","1","1","1","2"])
  assert.equal(new Set(await f.locator("#counts .count span").allTextContents()).size,8)
  assert.equal(await f.locator("#counts strong").allTextContents().then(v=>v.reduce((n,s)=>n+Number(s),0)),10)
  const rowsBefore=await f.locator(".row-title").allTextContents()
  assert.ok(rowsBefore.some(s=>s.startsWith("<img")))
  assert.equal(await f.locator("img,script[src]").count(),0)
  assert.ok((await f.locator(".row-project").allTextContents()).includes(zh?"無 Project":"No Project"))
  assert.equal(await page.evaluate(()=>fixture.navigations.length),0)
  assert.equal(await f.locator("body").evaluate(b=>b.scrollWidth<=b.clientWidth),true,"No whole-page horizontal overflow")
  assert.equal(await f.locator("body").evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)
  assert.equal(await f.locator("body").evaluate(b=>b.classList.contains("no-motion")),true)
  const touch=await f.locator(".row, .control, #openSession").evaluateAll(es=>es.every(e=>e.getBoundingClientRect().height>=44))
  assert.equal(touch,true,"Controls retain touch-sized heights")
  await page.screenshot({path:`${artifacts}/synthetic-${engine}-${width}-${lang}-instrument.png`,fullPage:true})
  // Selection updates the eyepiece without navigation; stale/unknown retain text and refuse opening.
  await f.locator(".row").filter({hasText:"<img src=x"}).click()
  assert.equal(await f.locator("#openSession").isDisabled(),true)
  await f.locator(".row").filter({hasText:"A synthetic Session without a Project"}).click()
  assert.equal(await f.locator("#openSession").isDisabled(),true)
  await f.locator("#list").click()
  assert.deepEqual(await f.locator(".row-title").allTextContents(),rowsBefore)
  assert.ok((await f.locator("#detail").textContent()).includes("A synthetic Session without a Project"))
  await page.screenshot({path:`${artifacts}/synthetic-${engine}-${width}-${lang}-list.png`,fullPage:true})
  await f.locator("#list").click()
  // Same Project/Session retains its position across unrelated snapshot changes.
  const point=f.locator('.planet[aria-label*="Shape the new workspace"]')
  const before=await point.getAttribute("style")
  await page.evaluate(()=>{fixture.rows[1].title="Updated waiting Session";fixture.change()})
  await f.locator(".row-title").getByText("Updated waiting Session",{exact:true}).waitFor()
  assert.equal(await point.getAttribute("style"),before)
  await page.emulateMedia({reducedMotion:"no-preference"})
  await f.locator("body:not(.no-motion)").waitFor()
  await page.evaluate(()=>{fixture.rows[3].workState="waiting_you";fixture.change()})
  await f.locator('.planet.change[data-state="waiting_you"]').waitFor()
  const changed=f.locator('.planet.change[data-state="waiting_you"]')
  assert.equal(await changed.evaluate(b=>getComputedStyle(b).transitionDuration.split(",").every(s=>s.trim()==="0.7s")),true)
  assert.ok(await changed.evaluate(b=>b.getAnimations().length>0),"Actual state transition is active")
  await page.emulateMedia({reducedMotion:"reduce"})
  await f.locator("body.no-motion").waitFor()
  assert.equal(await f.locator(".planet").evaluateAll(bs=>bs.every(b=>b.getAnimations().length===0)),true)
  await page.emulateMedia({reducedMotion:"no-preference"})
  await f.locator("body:not(.no-motion)").waitFor()
  await page.evaluate(()=>{fixture.rows[3].workState="working";fixture.change()})
  await f.locator('.planet.change[data-state="working"]').waitFor()
  await page.evaluate(()=>{fixture.rows[3].freshness="stale";fixture.change()})
  await point.and(f.locator('[data-freshness="stale"]')).waitFor()
  assert.equal(await f.locator('.planet[data-freshness="stale"]').evaluateAll(bs=>bs.every(b=>b.getAnimations().length===0)),true)
  await page.evaluate(()=>{fixture.rows[3].freshness="fresh";fixture.change()})
  await f.locator("#still").click()
  assert.equal(await f.locator("body").evaluate(b=>b.classList.contains("no-motion")),true)
  assert.equal(await f.locator(".planet").evaluateAll(bs=>bs.every(b=>b.getAnimations().length===0)),true)
  await f.locator("#still").click()
  await page.evaluate(()=>fixture.hidden(true))
  await f.locator("body.no-motion").waitFor()
  assert.equal(await f.locator(".planet").evaluateAll(bs=>bs.every(b=>b.getAnimations().length===0)),true)
  await page.evaluate(()=>fixture.hidden(false))
  await f.locator("body:not(.no-motion)").waitFor()
  await page.evaluate(()=>fixture.disconnect())
  await f.locator("#warning").waitFor()
  assert.equal(await f.locator("#openSession").isDisabled(),true)
  assert.equal(await f.locator("body").evaluate(b=>b.classList.contains("no-motion")),true)
  await page.evaluate(()=>fixture.reconnect())
  await f.locator("body:not(.no-motion)").waitFor()
  // A dense complete snapshot uses the same readable register, without truncating rows.
  await page.evaluate(()=>{for(let i=0;i<30;i++)fixture.rows.push({...fixture.rows[i%8],key:"dense-"+i,title:"Synthetic dense Session "+i});fixture.change()})
  await f.locator(".row").nth(39).waitFor()
  assert.equal(await f.locator(".planet").count(),40)
  assert.equal(await f.locator("body").evaluate(b=>b.scrollWidth<=b.clientWidth),true)
  await page.screenshot({path:`${artifacts}/synthetic-${engine}-${width}-${lang}-dense.png`,fullPage:true})
  await page.evaluate(()=>{fixture.saved=[...fixture.rows];fixture.rows.splice(0);fixture.change()})
  await f.locator("#empty").waitFor()
  assert.equal(await f.locator(".planet").count(),0)
  assert.equal(await f.locator("#openSession").count(),0)
  await page.evaluate(()=>{fixture.rows.push(...fixture.saved);fixture.change()})
  await f.locator(".row").nth(39).waitFor()
  await f.locator(".row").filter({hasText:"Package ready for review"}).click()
  assert.ok((await f.locator("#detail").textContent()).includes(zh?"尚不代表已驗收":"not acceptance"))
  assert.equal(await page.evaluate(()=>fixture.navigations.length),0)
  await f.locator("#openSession").click()
  await page.locator(".vp-host").waitFor({state:"hidden"})
  await page.waitForFunction(()=>fixture.navigations.length===1)
  assert.deepEqual(await page.evaluate(()=>fixture.navigations),["synthetic-5"])
  assert.deepEqual(errors,[])
  reports.push({engine,width,lang,passed:true,cases:["production-file-import","10-row-initial","8-state-counts","hostile-text","null-project","fresh-stale-unknown","selection-no-navigation","list-parity","stable-position","snapshot-update","event-only-transition","reduced-motion","stale-stops-transition","still-mode","simulated-host-visibility","disconnect-reconnect","40-row-density","empty-replace","explicit-navigation","no-horizontal-overflow","touch-controls"]})
 }catch(e){await page.screenshot({path:`${artifacts}/${engine}-${width}-failure.png`,fullPage:true});throw e}finally{await browser.close()}
}}finally{await new Promise(r=>server.close(r));await writeFile(`${artifacts}/results.json`,JSON.stringify(reports,null,2)+"\n")}
console.log(JSON.stringify(reports,null,2))
