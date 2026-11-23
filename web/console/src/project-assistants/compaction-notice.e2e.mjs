// A compaction notice reads in the reader's language alone, for a notice stored
// with a key and one stored earlier in both languages, and the context meter
// kept from a Codex Bot's first turn is shown once it ends.
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile} from "node:fs/promises"
import path from "node:path"
import {pathToFileURL} from "node:url"
import {build} from "esbuild"

const {PLAYWRIGHT_MODULE,BOTS_ARTIFACTS,CHROME}=process.env
assert.ok(PLAYWRIGHT_MODULE&&BOTS_ARTIFACTS,"Set PLAYWRIGHT_MODULE and BOTS_ARTIFACTS")
const {chromium}=await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
await mkdir(BOTS_ARTIFACTS,{recursive:true})
const bundle=await build({
  entryPoints:[new URL("fixtures.ts",import.meta.url).pathname],
  outfile:"project-assistants-fixture.js",write:false,bundle:true,format:"esm",platform:"browser",
  jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}
})
const files=new Map(bundle.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const html=await readFile(new URL("../../tools/fixtures/project-assistants.html",import.meta.url))
const catalogs=new Map(await Promise.all(["en","zh-Hant"].map(async tag=>[
  `/catalog/${tag}.json`,await readFile(new URL(`../../public/catalogs/${tag}.json`,import.meta.url))
])))
const server=createServer((req,res)=>{
  const {pathname}=new URL(req.url,"http://fixture")
  const file=files.get(pathname)
  if(file){res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript");res.end(file);return}
  if(catalogs.has(pathname)){res.setHeader("Content-Type","application/json");res.end(catalogs.get(pathname));return}
  if(pathname==="/"){res.setHeader("Content-Type","text/html");res.end(html);return}
  res.writeHead(404).end()
})
await new Promise((resolve,reject)=>server.once("error",reject).listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
let browser
try{
  browser=await chromium.launch({headless:true,executablePath:CHROME,args:["--disable-features=OmniboxEverywhere"]})
  for(const language of ["en","zh-Hant"]){
    const context=await browser.newContext({viewport:{width:390,height:844},deviceScaleFactor:2,reducedMotion:"reduce"})
    try{
      const page=await context.newPage()
      page.setDefaultTimeout(10000)
      const errors=[]
      page.on("pageerror",error=>errors.push(error.message))
      await page.addInitScript(tag=>localStorage.setItem("ui_language",tag),language)
      await page.goto(origin+"/?lang="+language+"&header=1")
      await page.waitForFunction(()=>!!window.botsFixture)
      await page.evaluate(()=>{window.botsFixture.compactionFixture();window.botsFixture.open()})
      await page.locator('.pa-bot-row[data-bot-key=\'["studio","bot-2"]\']').click()
      const notices=page.locator(".pa-notice p")
      await notices.first().waitFor()
      const texts=await notices.allInnerTexts()
      const zh=language==="zh-Hant"
      assert.deepEqual(texts,zh?["已摘要較早的內容，騰出空間：345K → 41K","已摘要較早的內容，騰出空間：26.2K → 22.1K"]:["Earlier context was summarized to make room: 345K → 41K","Earlier context was summarized to make room: 26.2K → 22.1K"])
      for(const text of texts)assert.ok(zh?!/[A-Za-z]{3}/.test(text):!/[一-鿿]/.test(text),`one language: ${text}`)
      const meter=page.locator(".pa-context-usage")
      await meter.waitFor()
      assert.match(await meter.innerText(),/22K\s*\/\s*475K/)
      assert.deepEqual(errors,[])
      await page.screenshot({path:path.join(BOTS_ARTIFACTS,`compaction-notice-${language}-390.png`)})
      console.log(language,JSON.stringify(texts),(await meter.innerText()).replace(/\s+/g," "))
    }finally{await context.close()}
  }
}finally{await browser?.close();server.close()}
