/** Dev server rows in a real browser against the synthetic fixture: on touch the "⋯" menu answers a 44px square and its glyph is drawn as on a pointer device. No daemon, Cloud route, model or real command runs. */
import assert from "node:assert/strict"
import {spawn} from "node:child_process"
import {mkdir} from "node:fs/promises"
import path from "node:path"
import {fileURLToPath,pathToFileURL} from "node:url"
const {PLAYWRIGHT_MODULE,BOTS_ARTIFACTS:artifacts}=process.env
assert.ok(PLAYWRIGHT_MODULE&&artifacts,"Set BOTS_ARTIFACTS and an existing PLAYWRIGHT_MODULE")
await mkdir(artifacts,{recursive:true})
const pw=await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
const fixture=spawn(process.execPath,[fileURLToPath(new URL("project-assistants-dev-servers.fixture.mjs",import.meta.url))],{stdio:["ignore","pipe","inherit"],env:process.env})
const exited=new Promise(resolve=>fixture.once("exit",code=>resolve(code)))
const {origin}=await new Promise((resolve,reject)=>{
  let text="";fixture.stdout.on("data",chunk=>{text+=chunk;const line=text.split("\n").find(l=>l.startsWith("{"));if(line)resolve(JSON.parse(line))})
  exited.then(code=>reject(Error(`the dev server fixture exited (${code}) before it served`)))
})
const problems=[],glyphs=new Map()
try{
  for(const engine of (process.env.BOTS_ENGINES??"chromium,webkit").split(",")){
    const browser=await pw[engine].launch({headless:true,...(engine==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{})})
    try{for(const [width,height] of [[390,844],[1280,800]]){
      const touch=width===390,label=`${engine} ${width}x${height}`
      const context=await browser.newContext({viewport:{width,height},hasTouch:touch,isMobile:touch&&engine==="chromium",reducedMotion:"reduce"}),page=await context.newPage()
      await page.goto(`${origin}/?lang=zh-Hant`)
      const menus=page.locator(".pa-dev-server .pa-server-menu")
      await menus.first().waitFor({state:"attached"})
      await page.waitForFunction(()=>document.querySelectorAll(".pa-dev-server .pa-server-menu").length>=4)
      if(!touch)await page.locator(".pa-dev-server").first().hover()
      await page.screenshot({path:path.join(artifacts,`dev-servers-${engine}-${width}.png`),clip:{x:0,y:0,width,height:Math.min(height,420)}})
      for(let i=0;i<await menus.count();i++){
        const seen=await menus.nth(i).evaluate((menu,touch)=>{
          const r=menu.getBoundingClientRect(),c={x:r.left+r.width/2,y:r.top+r.height/2}
          // The button is round, so its corners are not part of it; the edges of a 44px square are.
          const misses=touch?[[0,-21],[0,21],[-21,0],[21,0]].filter(([dx,dy])=>document.elementFromPoint(c.x+dx,c.y+dy)?.closest("button")!==menu):[]
          const range=document.createRange();range.selectNodeContents(menu);const g=range.getBoundingClientRect()
          return {width:r.width,height:r.height,glyph:[Math.round(g.width*10)/10,Math.round(g.height*10)/10],misses,name:menu.getAttribute("aria-label")}
        },touch)
        if(!touch&&(Math.abs(seen.width-36)>0.5||Math.abs(seen.height-36)>0.5))problems.push(`${label}: ${seen.name} keeps its 36px box on a pointer device (${seen.width}x${seen.height})`)
        const key=`${engine} ${seen.name}`
        if(glyphs.has(key)&&JSON.stringify(glyphs.get(key))!==JSON.stringify(seen.glyph))problems.push(`${label}: ${seen.name}'s glyph looks the same on touch and pointer (${JSON.stringify(glyphs.get(key))} vs ${JSON.stringify(seen.glyph)})`)
        glyphs.set(key,seen.glyph)
        if(touch&&(seen.width<44||seen.height<44))problems.push(`${label}: ${seen.name} is at least 44px on touch (${seen.width}x${seen.height})`)
        if(seen.misses.length)problems.push(`${label}: ${seen.name} answers a 44px touch target (missed at ${JSON.stringify(seen.misses)})`)
      }
      console.log(`ok ${label}: ${await menus.count()} menus measured`)
      await context.close()
    }}finally{await browser.close()}
  }
}finally{fixture.kill("SIGTERM");await exited}
assert.deepEqual(problems,[],"every dev server menu keeps its size and touch target")
