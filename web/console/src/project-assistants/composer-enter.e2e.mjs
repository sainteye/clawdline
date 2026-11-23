/** Bot composer keys: Enter sends with a mouse and keyboard, and is a new line on a touch screen. No daemon, provider or paid API. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"
const artifacts=process.env.BOTS_ARTIFACTS
if(!artifacts||!process.env.PLAYWRIGHT_MODULE)throw Error("Set BOTS_ARTIFACTS and an existing PLAYWRIGHT_MODULE")
await mkdir(artifacts,{recursive:true})
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const built=await build({entryPoints:[fileURLToPath(new URL("./composer-enter.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"composer.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const server=createServer((req,res)=>{
 const file=files.get(new URL(req.url,"http://fixture").pathname)
 if(file){res.setHeader("Content-Type",req.url.endsWith("css")?"text/css":"text/javascript");res.end(file);return}
 res.setHeader("Content-Type","text/html");res.end(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#111;color:#eee;font-family:system-ui}</style><link rel="stylesheet" href="/composer.css"><div id="app"></div><script type="module" src="/composer.js"></script>`)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const failures=[]
async function check(label,run){try{await run();console.log(`ok ${label}`)}catch(error){failures.push(`${label}: ${error.message}`);console.log(`not ok ${label}: ${error.message.split("\n")[0]}`)}}
try{
 for(const engine of (process.env.BOTS_ENGINES??"chromium,webkit").split(",")){
  const browser=await pw[engine].launch({headless:true,...(engine==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{})})
  const open=async({touch=false,query=""}={})=>{
   const context=await browser.newContext({viewport:touch?{width:390,height:844}:{width:1280,height:800},hasTouch:touch,isMobile:touch&&engine==="chromium"})
   const page=await context.newPage()
   await page.goto(`${origin}/${query}`)
   const editor=page.getByRole("textbox",{name:"Message"})
   await editor.waitFor()
   return {context,page,editor,sends:()=>page.evaluate(()=>window.composer.sends)}
  }
  try{
   const desk=await open()
   const fine=await desk.page.evaluate(()=>matchMedia("(hover: hover) and (pointer: fine)").matches)
   if(fine){
    await check(`${engine} desktop: Enter sends`,async()=>{
     await desk.editor.fill("hello");await desk.editor.press("Enter")
     assert.deepEqual(await desk.sends(),["hello"]);assert.equal(await desk.editor.inputValue(),"")
     assert.equal(await desk.editor.getAttribute("enterkeyhint"),"send")
    })
    await check(`${engine} desktop: Enter while a send is busy neither resends nor adds a line`,async()=>{
     await desk.editor.fill("second");await desk.editor.press("Enter")
     assert.deepEqual(await desk.sends(),["hello"]);assert.equal(await desk.editor.inputValue(),"second")
     await desk.page.evaluate(()=>window.release())
    })
    await check(`${engine} desktop: Shift+Enter is a new line`,async()=>{
     await desk.editor.fill("line");await desk.editor.press("Shift+Enter")
     assert.equal(await desk.editor.inputValue(),"line\n");assert.deepEqual(await desk.sends(),["hello"])
    })
    await check(`${engine} desktop: Enter during input-method composition confirms, not sends`,async()=>{
     await desk.editor.fill("中")
     await desk.editor.evaluate(el=>el.dispatchEvent(new CompositionEvent("compositionstart",{bubbles:true,data:"中"})))
     await desk.editor.press("Enter")
     await desk.editor.evaluate(el=>el.dispatchEvent(new CompositionEvent("compositionend",{bubbles:true,data:"中"})))
     assert.deepEqual(await desk.sends(),["hello"])
     // A keydown that reports itself as composing, as Safari's last Enter of a word does.
     const prevented=await desk.editor.evaluate(el=>!el.dispatchEvent(new KeyboardEvent("keydown",{key:"Enter",bubbles:true,cancelable:true,isComposing:true})))
     assert.equal(prevented,false);assert.deepEqual(await desk.sends(),["hello"])
    })
    await check(`${engine} desktop: Enter picks the open @ mention instead of sending`,async()=>{
     await desk.editor.fill("");await desk.editor.pressSequentially("ask @Rev")
     await desk.page.getByRole("listbox").waitFor()
     await desk.editor.press("Enter")
     assert.match(await desk.editor.inputValue(),/@Reviewer/);assert.deepEqual(await desk.sends(),["hello"])
    })
    await check(`${engine} desktop: Command or Control+Enter also sends`,async()=>{
     await desk.editor.fill("modified");await desk.editor.press(engine==="webkit"?"Meta+Enter":"Control+Enter")
     assert.deepEqual(await desk.sends(),["hello","modified"])
     await desk.page.evaluate(()=>window.release())
    })
    await desk.page.screenshot({path:path.join(artifacts,`composer-${engine}-desktop.png`)})
   }else failures.push(`${engine}: a desktop context did not report hover and a fine pointer`)
   await desk.context.close()
   const blocked=await open({query:"?blocked"})
   await check(`${engine} desktop: Enter when sending is unavailable does nothing`,async()=>{
    await blocked.editor.fill("waiting");await blocked.editor.press("Enter")
    assert.deepEqual(await blocked.sends(),[]);assert.equal(await blocked.editor.inputValue(),"waiting")
   })
   await blocked.context.close()
   const phone=await open({touch:true})
   const coarse=await phone.page.evaluate(()=>!matchMedia("(hover: hover) and (pointer: fine)").matches)
   if(coarse)await check(`${engine} touch: Enter is a new line`,async()=>{
    await phone.editor.fill("first");await phone.editor.press("Enter")
    assert.equal(await phone.editor.inputValue(),"first\n");assert.deepEqual(await phone.sends(),[])
    assert.equal(await phone.editor.getAttribute("enterkeyhint"),"enter")
    await phone.page.screenshot({path:path.join(artifacts,`composer-${engine}-touch.png`)})
   })
   else console.log(`skip ${engine} touch: this engine does not emulate a coarse pointer`)
   await phone.context.close()
  }finally{await browser.close()}
 }
}finally{server.close()}
if(failures.length)throw Error(failures.join("\n"))
