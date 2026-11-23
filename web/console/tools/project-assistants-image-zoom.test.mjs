import {test} from "node:test"
import assert from "node:assert/strict"
import {mkdtemp,rm} from "node:fs/promises"
import {tmpdir} from "node:os"
import path from "node:path"
import {imageZoomBundle,runImageZoomAcceptance} from "./project-assistants-image-zoom.e2e.mjs"

test("the picture viewer fixture builds the real ImageViewer and its styles",async()=>{
  const {html,files}=await imageZoomBundle()
  assert.ok(html.includes("user-scalable=no"),"The fixture keeps the console's non-zooming page")
  assert.ok(files.get("/zoom.js")?.length>0)
  assert.ok(files.get("/zoom.css")?.length>0)
})

// The browsers are not part of the repository; with an installed Playwright the suite drives them.
const playwrightModule=process.env.PLAYWRIGHT_MODULE
test("a Bot picture pinches, double-taps, pans, springs back and closes in chromium and webkit",
  {skip:playwrightModule?false:"PLAYWRIGHT_MODULE is not set; run npm run e2e:bots-image-zoom with it"},async()=>{
  const artifacts=await mkdtemp(path.join(tmpdir(),"image-zoom-"))
  try{
    const results=await runImageZoomAcceptance({artifacts,playwrightModule})
    assert.equal(results.length,4)
  }finally{await rm(artifacts,{recursive:true,force:true})}
})
