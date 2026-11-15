/** Exercise the production installer, registry, frame and SDK using synthetic sources only. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {readFile,mkdir,writeFile} from "node:fs/promises"
import {pathToFileURL,fileURLToPath} from "node:url"
import {crc32} from "node:zlib"
import {build} from "esbuild"

export async function runViewAcceptance({playwrightModule,artifacts,engines=["chromium","webkit"]}) {
 await mkdir(artifacts,{recursive:true})
 const result=await build({stdin:{contents:`
 import React,{useState} from "react";import{createRoot}from"react-dom/client";
 import{useViewPlugins}from"./view-plugins/Host";
 const rows=[{key:"first",title:"<img src=x onerror=alert(1)>",project:{key:"one",label:"Demo One"},workState:"waiting_you",freshness:"fresh"},{key:"second",title:"Background work",project:{key:"two",label:"Demo Two"},workState:"working",freshness:"fresh"},{key:"third",title:"Unassigned work",project:null,workState:"unknown",freshness:"unknown"}];
 let listeners=new Set(),connected=true,active=true,navigations=[];window.fixture={rows,navigations,change(){for(const fn of listeners)fn()},disconnect(){connected=false;this.change()},revoke(){active=false;window.renderHost()}};
 function Host(){const [,render]=useState(0);window.renderHost=()=>render(n=>n+1);const api=useViewPlugins(active?[{key:"studio",label:"Studio",async read(){return{sessions:[...rows],connected}},subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn)},async prepareOpen(key){return()=>navigations.push(key)}}]:[],document.documentElement.lang);return <><button id="outside">Original interface</button><div id="dock">{api.dock}{api.entry}</div>{api.content}</>};createRoot(document.getElementById("root")).render(<Host/>);
 `,resolveDir:fileURLToPath(new URL("../src",import.meta.url)),loader:"tsx"},bundle:true,write:false,outfile:"host.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
 let forbiddenRequests=0
 const files=new Map(result.outputFiles.map(f=>["/"+f.path.split("/").at(-1),f.text]))
 const server=createServer(async(req,res)=>{try{const u=new URL(req.url,"http://test");let body;if(u.pathname==="/")body=`<!doctype html><html lang="${u.searchParams.get("lang")||"en"}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><div id="root"></div><script type="module" src="/host.js"></script>`;
 else if(/^\/view-plugins\/(session-galaxy|session-list)\.zip$/.test(u.pathname))body=await readFile(new URL("../public"+u.pathname,import.meta.url));else body=files.get(u.pathname);
 if(u.pathname.startsWith("/v1/"))forbiddenRequests++
 if(body===undefined){res.writeHead(404);res.end();return}res.setHeader("Content-Type",u.pathname.endsWith(".js")?"text/javascript":u.pathname.endsWith(".css")?"text/css":u.pathname.endsWith(".zip")?"application/zip":"text/html");res.end(body)}catch(e){res.writeHead(500);res.end(String(e))}})
 await new Promise(r=>server.listen(0,"127.0.0.1",r));const origin=`http://127.0.0.1:${server.address().port}`
 const pw=await import(pathToFileURL(playwrightModule).href),reports=[]
 try {for(const engine of engines)for(const width of [390,1440])for(const lang of ["en","zh-Hant"]){
 const browser=await pw[engine].launch({headless:true}),page=await browser.newPage({viewport:{width,height:900},reducedMotion:"reduce"}),errors=[]
 page.on("pageerror",e=>errors.push(e.message))
 try{
 await page.goto(origin+"/?lang="+lang)
 const zh=lang!=="en",entry=zh?"開啟視圖擴充":"Open view plugins",close=zh?"關閉":"Close",open=zh?"開啟":"Open",allow=zh?"允許並開啟":"Allow and open"
 await page.getByRole("button",{name:entry,exact:true}).click()
 await page.getByRole("button",{name:zh?"安裝星系":"Install Galaxy",exact:true}).click()
 await page.getByRole("button",{name:zh?"安裝套件":"Install package",exact:true}).click()
 await page.getByRole("button",{name:open,exact:true}).click()
 await page.getByRole("button",{name:allow,exact:true}).click()
 const frame=page.frameLocator("iframe")
 await frame.locator(".node").first().waitFor()
 assert.equal(await frame.locator(".node").count(),3)
 await frame.getByRole("button",{name:zh?"清單":"List",exact:true}).click()
 await frame.locator(".list-item").first().waitFor()
 assert.equal(await frame.locator("img").count(),0,"Titles are text, never markup")
 assert.equal(await page.locator("iframe").getAttribute("sandbox"),"allow-scripts")
 const isolation=await frame.locator("body").evaluate(async(el,url)=>{let parentBlocked=false;try{void parent.document.body}catch{parentBlocked=true}let networkBlocked=false;try{await fetch(url+"/v1/forbidden")}catch{networkBlocked=true}return{parentBlocked,networkBlocked}},origin)
 assert.deepEqual(isolation,{parentBlocked:true,networkBlocked:true});assert.equal(forbiddenRequests,0)
 await page.evaluate(()=>window.postMessage({type:"clawdline.plugin.hello",protocolVersion:1},"*"))
 await page.emulateMedia({reducedMotion:"no-preference"})
 const canvas=frame.locator("canvas.scene"),before=await canvas.screenshot();await page.waitForTimeout(180);const after=await canvas.screenshot()
 assert.notDeepEqual(before,after,"An opted-in visible scene draws motion")
 await page.emulateMedia({reducedMotion:"reduce"})
 await page.screenshot({path:`${artifacts}/${engine}-${width}-${lang}-galaxy.png`})
 await page.evaluate(()=>history.back())
 await page.locator(".vp-host").waitFor({state:"hidden"})
 assert.equal(await page.locator("#outside").evaluate(el=>el.inert),false)
 await page.waitForFunction(label=>document.activeElement?.getAttribute("aria-label")===label,entry)
 await page.getByRole("button",{name:entry,exact:true}).click()
 await page.getByRole("button",{name:zh?"安裝清單":"Install List",exact:true}).click()
 await page.getByRole("button",{name:zh?"安裝套件":"Install package",exact:true}).click()
 const list=page.locator(".vp-card").filter({has:page.getByRole("heading",{name:zh?"Session 清單":"Session List",exact:true})})
 await list.getByRole("button",{name:open,exact:true}).click()
 await page.getByRole("combobox").nth(1).selectOption("one")
 await page.getByRole("button",{name:allow,exact:true}).click()
 await frame.locator(".item").waitFor();assert.equal(await frame.locator(".item").count(),1)
 await page.evaluate(()=>fixture.disconnect())
 await frame.getByText(zh?"Demo One · 資料可能過時":"Demo One · May be stale",{exact:true}).waitFor()
 await page.evaluate(()=>fixture.revoke())
 await page.locator("iframe").waitFor({state:"detached"})
 await page.getByRole("alert").waitFor()
 await page.getByRole("button",{name:close,exact:true}).click()
 await page.locator(".vp-host").waitFor({state:"hidden"})
 await page.reload();await page.getByRole("button",{name:entry,exact:true}).click()
 await page.waitForFunction(()=>[...document.querySelectorAll(".vp-card h3")].length===2)
 assert.equal(await page.getByRole("button",{name:open,exact:true}).count(),2,"Both packages survive a reload; neither starts without a new grant")
 assert.equal(await page.locator("iframe").count(),0)
 const updated=Buffer.from(await readFile(new URL("../public/view-plugins/session-galaxy.zip",import.meta.url)))
 const version=updated.indexOf(Buffer.from('"version": "0.1.0"'));assert.ok(version>=0);updated.write('"version": "0.2.0"',version)
 const start=30+updated.readUInt16LE(26)+updated.readUInt16LE(28),size=updated.readUInt32LE(22),crc=crc32(updated.subarray(start,start+size))
 updated.writeUInt32LE(crc,14);const central=updated.indexOf(Buffer.from([0x50,0x4b,0x01,0x02]));updated.writeUInt32LE(crc,central+16)
 await page.locator('input[type=file]').setInputFiles({name:"updated.zip",mimeType:"application/zip",buffer:updated})
 await page.getByRole("button",{name:zh?"取代已安裝版本":"Replace installed version",exact:true}).click()
 const galaxy=page.locator(".vp-card").filter({has:page.getByRole("heading",{name:zh?"Session 星系":"Session Galaxy",exact:true})})
 await galaxy.getByText("com.clawdline.session-galaxy · 0.2.0",{exact:true}).waitFor()
 await galaxy.getByRole("button",{name:zh?"回退版本 · 0.1.0":"Roll back · 0.1.0",exact:true}).click()
 await galaxy.getByText("com.clawdline.session-galaxy · 0.1.0",{exact:true}).waitFor()
 await page.locator('input[type=file]').setInputFiles({name:"broken.zip",mimeType:"application/zip",buffer:Buffer.from("invalid")})
 await page.getByRole("alert").waitFor();await galaxy.getByText("com.clawdline.session-galaxy · 0.1.0",{exact:true}).waitFor()
 await galaxy.getByRole("button",{name:zh?"停用":"Disable",exact:true}).click()
 await galaxy.getByRole("button",{name:zh?"啟用":"Enable",exact:true}).waitFor()
 assert.equal(await galaxy.getByRole("button",{name:open,exact:true}).isDisabled(),true)
 await galaxy.getByRole("button",{name:zh?"移除":"Remove",exact:true}).click();await galaxy.waitFor({state:"detached"})
 assert.deepEqual(errors,[])
 reports.push({engine,width,lang,passed:true})
 }catch(e){await page.screenshot({path:`${artifacts}/failure.png`});console.error(await page.locator("body").innerHTML());throw e}finally{await browser.close()}
 }}finally{await new Promise(r=>server.close(r))}
 await writeFile(`${artifacts}/results.json`,JSON.stringify(reports,null,2));return reports
}
if(process.argv[1]===fileURLToPath(import.meta.url))console.log(await runViewAcceptance({playwrightModule:process.env.PLAYWRIGHT_MODULE,artifacts:process.env.PLUGIN_ARTIFACTS}))
