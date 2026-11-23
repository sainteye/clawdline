/** A row's right column keeps the time on the name's line and every badge on the preview's line,
 * whatever combination of dispatched count, read action and unread count it shows.
 * Same records and viewports before and after. No daemon, provider or paid API. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,writeFile} from "node:fs/promises"
import {execFileSync} from "node:child_process"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"
const artifacts=process.env.BOTS_ARTIFACTS
if(!artifacts||!process.env.PLAYWRIGHT_MODULE)throw Error("Set BOTS_ARTIFACTS and an existing PLAYWRIGHT_MODULE")
await mkdir(artifacts,{recursive:true})
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const baselineRef=process.env.BOTS_BASELINE??"5111f828405dd9580c2c4192e5a3639df19dad04"
const baseline=["BotRow.tsx","styles.css","roster-controls.css","dispatched-work.css"]
const variants=new Map()
for(const variant of ["before","after"]){
 const built=await build({entryPoints:[fileURLToPath(new URL("./roster-meta.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"meta.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'},plugins:variant==="before"?[{name:"exact-base-row",setup(builder){builder.onLoad({filter:/project-assistants\/(?:BotRow\.tsx|styles\.css|roster-controls\.css|dispatched-work\.css)$/},args=>{const file=path.basename(args.path);if(!baseline.includes(file))return;return {contents:execFileSync("git",["show",baselineRef+":web/console/src/project-assistants/"+file],{encoding:"utf8"}),loader:file.endsWith("css")?"css":"tsx",resolveDir:path.dirname(args.path)}})}}]:[]})
 variants.set(variant,new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents])))
}
const server=createServer((req,res)=>{
 const url=new URL(req.url,"http://fixture"),variant=url.pathname.split("/")[1],file=variants.get(variant)?.get("/"+url.pathname.split("/").at(-1))
 if(file){res.setHeader("Content-Type",url.pathname.endsWith("css")?"text/css":"text/javascript");res.end(file);return}
 res.setHeader("Content-Type","text/html");res.end(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#111;color:#eee;font-family:system-ui}</style><link rel="stylesheet" href="/${variant}/meta.css"><div id="app"></div><script type="module" src="/${variant}/meta.js"></script>`)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const measure=page=>page.evaluate(()=>{
 const box=el=>{if(!el)return null;const r=el.getBoundingClientRect();return {top:r.top,bottom:r.bottom,left:r.left,right:r.right,height:r.height,mid:(r.top+r.bottom)/2}}
 return [...document.querySelectorAll(".pa-roster-entry")].map(entry=>{
  const row=entry.querySelector(".pa-bot-row"),preview=row.querySelector(".pa-row-preview>span:last-child")
  return {name:row.querySelector("strong").textContent,long:preview.textContent.startsWith("A long reply"),project:!!row.querySelector(".pa-row-project"),row:box(row),name_:box(row.querySelector("strong")),preview:box(preview),previewTruncated:preview.scrollWidth>preview.clientWidth,
   time:box(row.querySelector("time")),clones:box(row.querySelector(".pa-clones")),unread:box(row.querySelector(".pa-unread")),read:box(entry.querySelector(".pa-roster-read svg"))}
 })
})
/** Every failure is a sentence, so a red run says which promise broke. */
function problems(rows){
 const out=[]
 if(!rows.some(r=>r.name==="Everything"))out.push("no row shows every badge")
 for(const r of rows){
  // A row is compared with the shortest row of the same shape: two lines, or three with a project line.
  const plain=Math.min(...rows.filter(o=>o.project===r.project).map(o=>o.row.height))
  if(r.row.height>plain+1)out.push(`${r.name}: row ${r.row.height}px is taller than the plain row ${plain}px`)
  if(Math.abs(r.time.mid-r.name_.mid)>3)out.push(`${r.name}: time centre ${r.time.mid.toFixed(1)} is off the name ${r.name_.mid.toFixed(1)}`)
  const badges=[["clones",r.clones],["unread",r.unread],["read",r.read]].filter(([,b])=>b)
  for(const [label,b] of badges){
   if(Math.abs(b.mid-r.preview.mid)>3)out.push(`${r.name}: ${label} centre ${b.mid.toFixed(1)} is off the preview line ${r.preview.mid.toFixed(1)}`)
   if(b.left<r.preview.right)out.push(`${r.name}: ${label} starts at ${b.left.toFixed(1)}, inside the preview that ends at ${r.preview.right.toFixed(1)}`)
   if(b.bottom>r.row.bottom||b.top<r.row.top)out.push(`${r.name}: ${label} leaves the row`)
  }
  const sorted=badges.map(([,b])=>b).sort((a,b)=>a.left-b.left)
  for(let i=1;i<sorted.length;i++)if(sorted[i].left<sorted[i-1].right)out.push(`${r.name}: badges overlap`)
  if(r.long&&!r.previewTruncated)out.push(`${r.name}: the long preview is not truncated`)
 }
 return out
}
const evidence=[]
try{
 for(const engine of (process.env.BOTS_ENGINES??"chromium,webkit").split(",")){
  const browser=await pw[engine].launch({headless:true,...(engine==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{})})
  try{for(const [language,width,height] of [["zh-Hant",390,720],["en",1440,900]]){
   for(const order of ["default","unread","machine"])for(const variant of ["before","after"]){
    const context=await browser.newContext({viewport:{width,height},hasTouch:width===390}),page=await context.newPage(),errors=[]
    page.on("pageerror",error=>errors.push(String(error)))
    await page.goto(`${origin}/${variant}/?lang=${language}&order=${order}`)
    await page.locator(".pa-roster-entry").nth(3).waitFor();await page.locator(".pa-roster-read").first().waitFor()
    const rows=await measure(page),found=problems(rows)
    await page.screenshot({path:path.join(artifacts,`${engine}-${width}-${order}-${variant}.png`)})
    for(const [i,r] of rows.entries())await page.locator(".pa-roster-entry").nth(i).screenshot({path:path.join(artifacts,`${engine}-${width}-${order}-${variant}-${r.name}.png`)})
    evidence.push({engine,language,width,order,variant,rows:rows.map(r=>({name:r.name,height:r.row.height})),problems:found,errors})
    assert.deepEqual(errors,[])
    if(variant==="after")assert.deepEqual(found,[],`${engine} ${width}px ${order}`)
    await context.close()
   }
  }}finally{await browser.close()}
 }
}finally{
 server.close()
 await writeFile(path.join(artifacts,"roster-meta.json"),JSON.stringify(evidence,null,2)+"\n")
}
const before=evidence.filter(e=>e.variant==="before")
console.log(JSON.stringify(evidence.map(e=>({engine:e.engine,width:e.width,order:e.order,variant:e.variant,heights:e.rows.map(r=>r.height).join("/"),problems:e.problems.length}))))
if(before.every(e=>e.problems.length===0))console.log("note: the baseline showed no problem")
