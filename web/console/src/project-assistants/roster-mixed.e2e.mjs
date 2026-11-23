/** Same records and CSS viewports, before and after. No daemon, provider or paid API. */
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
const baselineRef=process.env.BOTS_BASELINE??"be2ac16c571dac9ec2656fe786dd5fec4f3643f1"
const baseline=["BotRoster.tsx","BotRow.tsx","Groups.tsx","roster-controls.css"]
const variants=new Map()
for(const variant of ["before","after"]){
 const built=await build({entryPoints:[fileURLToPath(new URL("./roster-mixed.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"mixed.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'},plugins:variant==="before"?[{name:"exact-base-roster",setup(builder){builder.onLoad({filter:/project-assistants\/(?:BotRoster\.tsx|BotRow\.tsx|Groups\.tsx|roster-controls\.css)$/},args=>{const file=path.basename(args.path);if(!baseline.includes(file))return;const relative="web/console/src/project-assistants/"+file;return {contents:execFileSync("git",["show",baselineRef+":"+relative],{encoding:"utf8"}),loader:file.endsWith("css")?"css":"tsx",resolveDir:path.dirname(args.path)}})}}]:[]})
 variants.set(variant,new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents])))
}
const server=createServer((req,res)=>{
 const url=new URL(req.url,"http://fixture"),variant=url.pathname.split("/")[1],file=variants.get(variant)?.get("/"+url.pathname.split("/").at(-1))
 if(file){res.setHeader("Content-Type",req.url.endsWith("css")?"text/css":"text/javascript");res.end(file);return}
 res.setHeader("Content-Type","text/html");res.end(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#111;color:#eee;font-family:system-ui}</style><link rel="stylesheet" href="/${variant}/mixed.css"><div id="app"></div><script type="module" src="/${variant}/mixed.js"></script>`)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,evidence=[]
const metrics=page=>page.evaluate(()=>{
 const scroll=document.querySelector(".pa-roster-scroll"),bounds=scroll.getBoundingClientRect(),rows=[...document.querySelectorAll(".pa-roster-scroll [data-bot-key],.pa-roster-scroll [data-group-key]")]
 return {viewport:{width:innerWidth,height:innerHeight},listWidth:document.querySelector(".pa-roster").getBoundingClientRect().width,fullyVisibleRows:rows.filter(row=>{const r=row.getBoundingClientRect();return r.top>=bounds.top&&r.bottom<=bounds.bottom}).length,rows:rows.map(row=>({name:row.querySelector("strong")?.textContent,top:row.getBoundingClientRect().top,height:row.getBoundingClientRect().height})),handles:document.querySelectorAll(".pa-roster-handle").length,stars:document.querySelectorAll(".pa-roster-pin").length,emptyDrop:!!document.querySelector(".pa-roster-pin-target"),historyRequests:window.fixture.history}
})
try{
 for(const engine of (process.env.BOTS_ENGINES??"chromium,webkit").split(",")){
 const browser=await pw[engine].launch({headless:true,...(engine==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{})})
 try{for(const [language,width,height] of [["en",1440,900],["zh-Hant",390,720]]){
  const result={engine,language,width,height}
  for(const variant of ["before","after"]){
   const context=await browser.newContext({viewport:{width,height},hasTouch:width===390}),page=await context.newPage(),errors=[]
   page.on("pageerror",error=>errors.push(String(error)))
   await page.goto(`${origin}/${variant}/?lang=${language}`)
   const group=page.locator('[data-group-key]').filter({has:page.getByText("Release discussion",{exact:true})})
   await group.waitFor();await page.mouse.move(width-1,height-1)
   result[variant]=await metrics(page)
   await page.screenshot({path:path.join(artifacts,`${engine}-${language}-${variant}.png`)})
   if(variant==="after"){
    assert.equal(result.after.handles,0);assert.equal(result.after.stars,0);assert.equal(result.after.emptyDrop,false)
    assert.equal(await group.locator(".pa-avatar").count(),2)
    assert.match(await group.innerText(),/Two: The final group reply/)
    assert.match(await page.locator('[data-group-key]').filter({has:page.getByText("Older source without a preview",{exact:true})}).innerText(),/2/)
    assert.equal(await page.evaluate(()=>window.fixture.history),0)
    const row=group.locator("..")
    assert.equal(await row.locator(".pa-menu").count(),0)
    await group.focus();await page.keyboard.press("Shift+F10")
    await row.locator(".pa-roster-handle").waitFor()
    await row.locator(".pa-roster-pin").click()
    await page.getByRole("button",{name:language==="en"?"Done":"完成",exact:true}).click()
    await page.reload();await group.waitFor()
    assert.equal(await page.locator(".pa-project-group").first().locator(".pa-group-row strong").first().innerText(),"Release discussion")
    const options=page.getByRole("button",{name:language==="en"?"List options":"清單選項",exact:true})
    await options.click();await page.waitForFunction(()=>document.activeElement?.getAttribute("role")==="menuitem")
    await page.keyboard.press("Escape");assert.equal(await options.evaluate(el=>document.activeElement===el),true)
    await options.click();await page.getByRole("menuitem",{name:language==="en"?"Reorder conversations":"調整對話順序",exact:true}).click()
    assert.ok(await page.locator(".pa-roster-handle").count()>0)
    await page.getByRole("button",{name:language==="en"?"Done":"完成",exact:true}).click()
    await group.scrollIntoViewIfNeeded()
    const before=await page.locator(".pa-roster-scroll").evaluate(el=>el.scrollTop),started=performance.now()
    await group.click();await page.getByRole("dialog",{name:"Release discussion",exact:true}).waitFor()
    await page.getByText("The final group reply is ready for review.",{exact:true}).waitFor()
    const dialog=page.getByRole("dialog",{name:"Release discussion",exact:true}),rect=await dialog.boundingBox()
    result.flow={enterClicks:1,returnClicks:1,fixtureEnterMs:Math.round(performance.now()-started),roomBounds:rect,cssPosition:await dialog.evaluate(el=>getComputedStyle(el).position)}
    result.flow.roomWithinViewport=rect.y>=0&&rect.y+rect.height<=height&&rect.x>=0&&rect.x+rect.width<=width
    result.flow.legacyRoomAllowed=process.env.BOTS_ALLOW_LEGACY_ROOM==="1"
    await page.screenshot({path:path.join(artifacts,`${engine}-${language}-room.png`)})
    await writeFile(path.join(artifacts,`${engine}-${language}-flow.json`),JSON.stringify(result,null,2)+"\n")
    if(!result.flow.legacyRoomAllowed)assert.equal(result.flow.roomWithinViewport,true,"Room placement must be integrated before acceptance")
    await page.getByRole("button",{name:language==="en"?"Back":"返回",exact:true}).click()
    await page.waitForFunction(()=>!document.querySelector('[role="dialog"]'))
    await page.waitForFunction(()=>document.activeElement?.matches(".pa-group-row[data-group-key]"))
    assert.equal(await group.evaluate(el=>document.activeElement===el),true)
    assert.equal(await page.locator(".pa-roster-scroll").evaluate(el=>el.scrollTop),before)
    assert.equal(await page.evaluate(()=>window.fixture.writes),0)
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
   }
   assert.deepEqual(errors,[])
   if(variant==="after"){
    await page.goto(`${origin}/after/?lang=${language}&actions`)
    const candidate=page.locator(".pa-bot-row").filter({has:page.getByText("Code review",{exact:true})})
    const lead=page.getByRole("menuitem",{name:language==="en"?"Set as lead assistant":"設為主助理",exact:true})
    const deleteLabel=language==="en"?"Delete Bot":"刪除 Bot"
    const remove=page.getByRole("menuitem",{name:deleteLabel,exact:true})
    await candidate.click({button:"right"})
    await remove.waitFor()
    // A menu behind the Bots window is in the DOM but cannot receive input.
    const hit=await remove.evaluate(button=>{
     const box=button.getBoundingClientRect()
     return button.contains(document.elementFromPoint(box.x+box.width/2,box.y+box.height/2))
    })
    assert.ok(hit,"the real Bot window must not cover its context menu")
    await page.screenshot({path:path.join(artifacts,`${engine}-${language}-bot-context.png`)})
    await lead.click()
    await candidate.locator(".pa-roster-lead").waitFor()
    await candidate.focus();await page.keyboard.press("Shift+F10")
    await remove.waitFor();assert.equal(await lead.count(),0,"the current lead no longer needs the promotion action")
    await remove.click()
    const confirm=page.getByRole("dialog",{name:deleteLabel,exact:true})
    await confirm.getByRole("button",{name:language==="en"?"Cancel":"取消",exact:true}).click()
    assert.equal(await page.evaluate(()=>window.fixture.writes),0)
    await candidate.click({button:"right"});await remove.click()
    await confirm.getByRole("button",{name:deleteLabel,exact:true}).click()
    await candidate.waitFor({state:"detached"})
    assert.equal(await page.evaluate(()=>window.fixture.writes),1)
    result.rosterActions={rightClick:true,keyboardMenu:true,promoted:true,deleteConfirmed:true}
    const openAssistant=()=>page.locator(".pa-bot-row").filter({has:page.getByText("Project assistant",{exact:true})}).click()
    const startCall=page.locator(".pa-composer .pa-send[data-call]")
    await page.evaluate(()=>{
     for(const name of ["SpeechRecognition","webkitSpeechRecognition"])Object.defineProperty(window,name,{configurable:true,value:undefined})
    })
    await openAssistant()
    await page.locator(".pa-composer textarea:not(:disabled)").waitFor()
    await startCall.waitFor({state:"detached",timeout:2000})
    await page.goto(`${origin}/after/?lang=${language}&actions`)
    await page.evaluate(()=>{
     const voice={pack:false,localVoice:false,checks:0,opened:0,closed:0,say:null}
     const synth=new EventTarget()
     synth.getVoices=()=>voice.localVoice?[{lang:"en-US",localService:true},{lang:"zh-TW",localService:true}]:[]
     voice.update=()=>synth.dispatchEvent(new Event("voiceschanged"))
     class Recognition {
      processLocally=false
      static async available(){voice.checks++;return voice.pack?"available":"downloadable"}
      start(){voice.say=text=>this.onresult?.({resultIndex:0,results:[{isFinal:true,0:{transcript:text}}]})}
      abort(){voice.say=null}
     }
     Object.defineProperty(Recognition.prototype,"processLocally",{value:false})
     const track=Object.assign(new EventTarget(),{readyState:"live",enabled:false,stop:()=>voice.closed++})
     Object.defineProperty(window,"SpeechRecognition",{configurable:true,value:Recognition})
     Object.defineProperty(window,"speechSynthesis",{configurable:true,value:synth})
     Object.defineProperty(window,"SpeechSynthesisUtterance",{configurable:true,value:class {}})
     Object.defineProperty(navigator,"mediaDevices",{configurable:true,value:{getUserMedia:async()=>{
      voice.opened++;return {getAudioTracks:()=>[track],getTracks:()=>[track]}
     }}})
     window.fixtureVoice=voice
    })
    await openAssistant()
    await page.waitForFunction(()=>window.fixtureVoice.checks>0)
    await startCall.waitFor({state:"detached",timeout:2000})
    await page.evaluate(()=>{window.fixtureVoice.pack=true;window.fixtureVoice.update()})
    await page.waitForFunction(()=>window.fixtureVoice.checks>1)
    await startCall.waitFor({state:"detached",timeout:2000})
    await page.evaluate(()=>{window.fixtureVoice.localVoice=true;window.fixtureVoice.update()})
    await startCall.click()
    await page.locator(".pa-bot-call-status").filter({hasText:language==="en"?"Listening":"正在聆聽"}).waitFor()
    const spoken="A synthetic spoken request"
    await page.evaluate(text=>window.fixtureVoice.say(text),spoken)
    await page.locator(".pa-chat-content").getByText(spoken,{exact:true}).waitFor()
    await page.getByRole("button",{name:language==="en"?"Hang up":"掛斷",exact:true}).click()
    assert.deepEqual(await page.evaluate(()=>[window.fixtureVoice.opened,window.fixtureVoice.closed]),[1,1])
    result.botCall={hiddenWithoutSupport:true,hiddenWithoutLanguageOrVoice:true,heardAndSent:true,microphoneReleased:true}
    await page.goto(`${origin}/after/?lang=${language}&delayMembers`)
    const avatar=group.locator(".pa-group-avatars")
    const many=page.locator("[data-group-key]").filter({has:page.getByText("Six member room",{exact:true})})
    await group.waitFor()
    assert.doesNotMatch(await avatar.innerText(),/\?/,"Group loaded ahead of its member roster")
    assert.equal(await avatar.locator("svg").count(),1)
    assert.equal(await many.locator(".pa-group-more").innerText(),"+2")
    await page.evaluate(()=>window.fixture.setMemberCount(1))
    await avatar.locator(".pa-avatar").waitFor()
    assert.doesNotMatch(await avatar.innerText(),/\?/,"Only part of the member roster is available")
    await page.evaluate(()=>window.fixture.setMemberCount(6))
    await avatar.locator(".pa-avatar").nth(1).waitFor()
    assert.equal(await many.locator(".pa-avatar").count(),4)
    assert.deepEqual(errors,[])
   }
   await context.close()
  }
  evidence.push(result)
 }}finally{await browser.close()}
 }
 await writeFile(path.join(artifacts,"mixed-evidence.json"),JSON.stringify({baselineRef,modelRequests:0,evidence},null,2)+"\n")
 console.log(JSON.stringify(evidence))
}finally{server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
