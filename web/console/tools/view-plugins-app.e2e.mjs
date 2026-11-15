/** The full App and existing Session owners, using the established synthetic host fixture. */
import {hostBundle} from "./project-assistants-host.e2e.mjs"
import {createServer} from "node:http"
import {readFile,mkdir,writeFile} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import assert from "node:assert/strict"
export async function runAppPluginAcceptance({playwrightModule,artifacts}) {
 const {html,files}=await hostBundle();await mkdir(artifacts,{recursive:true})
 const server=createServer(async(req,res)=>{try{const url=new URL(req.url,"http://fixture");let body=files.get(url.pathname)
 if(url.pathname==="/")body=html
 if(/^\/catalogs\/(en|zh-Hant)\.json$/.test(url.pathname)||/^\/view-plugins\/session-galaxy\.zip$/.test(url.pathname))body=await readFile(new URL("../public"+url.pathname,import.meta.url))
 if(body===undefined){res.writeHead(404);res.end();return}
 res.setHeader("Content-Type",url.pathname.endsWith(".js")?"text/javascript":url.pathname.endsWith(".css")?"text/css":url.pathname.endsWith(".json")?"application/json":url.pathname.endsWith(".zip")?"application/zip":"text/html");res.end(body)}catch(e){res.writeHead(500);res.end(String(e))}})
 await new Promise(r=>server.listen(0,"127.0.0.1",r));const pw=await import(pathToFileURL(playwrightModule).href)
 const browser=await pw.chromium.launch({headless:true}),page=await browser.newPage({viewport:{width:390,height:900},reducedMotion:"reduce"}),errors=[]
 page.on("pageerror",e=>errors.push(e.stack || e.message))
 try{
 await page.goto(`http://127.0.0.1:${server.address().port}/?lang=zh-Hant`)
 await page.getByRole("button",{name:"開啟已安裝視圖",exact:true}).waitFor({timeout:10000})
 await page.evaluate(()=>{window.savedApp=document.querySelector("#app");document.querySelector("#list-scroll").scrollTop=220;window.fixtureHost.beginSyntheticCall()})
 await page.getByRole("button",{name:"開啟側邊欄",exact:true}).click()
 await page.locator("#nav-view-plugins").click()
 await page.getByRole("button",{name:"安裝星系",exact:true}).click()
 await page.getByRole("button",{name:"安裝套件",exact:true}).click()
 await page.getByRole("button",{name:"開啟",exact:true}).click()
 await page.frameLocator("iframe").locator(".node").first().waitFor()
 assert.equal(await page.frameLocator("iframe").locator(".node").count(),40)
 assert.equal(await page.getByRole("combobox").count(),0,"The first opening has no selection or consent panel")
 await page.screenshot({path:`${artifacts}/full-app-galaxy.png`})
 await page.evaluate(()=>history.back());await page.locator(".vp-host").waitFor({state:"hidden"})
 assert.equal(await page.evaluate(()=>savedApp===document.querySelector("#app")),true)
 assert.equal(await page.evaluate(()=>document.querySelector("#list-scroll").scrollTop),220)
 assert.equal(await page.evaluate(()=>fixtureHost.voiceState().status),"connected")
 assert.equal(await page.evaluate(()=>fixtureHost.stats().sessionSends),0)
 await page.getByRole("button",{name:"開啟已安裝視圖",exact:true}).click()
 await page.frameLocator("iframe").locator(".node").first().waitFor()
 assert.equal(await page.getByRole("combobox").count(),0,"The actual App shortcut opens every Project without scope selection")
 await page.getByRole("button",{name:"關閉視圖並返回",exact:true}).click();await page.locator(".vp-host").waitFor({state:"hidden"})
 assert.deepEqual(errors,[]);await writeFile(`${artifacts}/full-app-results.json`,JSON.stringify({passed:true,errors}))
 }catch(e){await page.screenshot({path:`${artifacts}/full-app-failure.png`});await writeFile(`${artifacts}/full-app-errors.json`,JSON.stringify({errors,html:await page.locator("body").innerHTML()}));throw e}
 finally{await browser.close();await new Promise(r=>server.close(r))}
}
if(process.argv[1]===fileURLToPath(import.meta.url))await runAppPluginAcceptance({playwrightModule:process.env.PLAYWRIGHT_MODULE,artifacts:process.env.PLUGIN_ARTIFACTS})
