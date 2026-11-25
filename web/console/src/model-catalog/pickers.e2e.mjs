// The Settings default-model picker and the Board's per-launch model row in a
// real browser, reading a synthetic daemon's model catalog: a `refreshing`
// answer that completes later, a failure that Retry recovers, and an older
// daemon that answers 404. Every opened list stays inside its container, at
// 1280 and 390 px.
// PLAYWRIGHT_MODULE=<playwright index.mjs> PICKERS_ARTIFACTS=<dir> node pickers.e2e.mjs
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile} from "node:fs/promises"
import path from "node:path"
import {pathToFileURL} from "node:url"
import {build} from "esbuild"

const {PLAYWRIGHT_MODULE,PICKERS_ARTIFACTS}=process.env
assert.ok(PLAYWRIGHT_MODULE&&PICKERS_ARTIFACTS,"Use an existing Playwright installation and a task-owned evidence directory")
await mkdir(PICKERS_ARTIFACTS,{recursive:true})
const browsers=await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
const bundle=await build({entryPoints:[new URL("pickers.fixture.tsx",import.meta.url).pathname],outdir:"out",entryNames:"pickers-fixture",write:false,bundle:true,format:"esm",platform:"browser",jsx:"automatic",
  loader:{".html":"text",".png":"dataurl",".svg":"dataurl",".woff2":"dataurl"},define:{"process.env.NODE_ENV":'"development"',"import.meta.env":"{}"}})
const files=new Map(bundle.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const html=`<!doctype html><html lang="zh-Hant"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Model pickers fixture</title><link rel="stylesheet" href="/pickers-fixture.css"><style>body{margin:0;padding:16px;background:var(--bg,#0b0b0b)}section{max-width:640px;margin:0 auto 24px;overflow:hidden;border:1px solid #333;border-radius:8px;padding:12px}</style></head><body><div id="fixture"></div><script type="module">import {mountFixture} from '/pickers-fixture.js';mountFixture();</script></body></html>`
const catalogs=new URL("../../public/catalogs/",import.meta.url)
const types={".css":"text/css",".json":"application/json"}

// The synthetic daemon. Per page (by the mode in its Referer): how many catalog reads it answered.
const state=new Map()
const row=(agent,defaultName,models,refreshing=false,source="agent",efforts=[])=>({agent,models:models.map(v=>({value:v.toLowerCase().replace(/\s+/g,"-"),name:v})),default_name:defaultName,efforts,source,observed_at:"2026-11-25T00:00:00Z",refreshing})
const fresh=()=>({codex_default_model:"model-retired",claude_default_model:"",codex_default_effort:"xhigh",antigravity_default_model:"",antigravity_default_effort:""})
let saved=fresh()
const unused={codex_default_model:"model-retired",claude_default_model:"",codex_default_effort:"xhigh",antigravity_default_model:"",antigravity_default_effort:""}
function catalogAnswer(mode,res){
  const s=state.get(mode)??{asked:0,started:Date.now()};state.set(mode,s);s.asked++
  if(mode==="older"){res.writeHead(404,{"Content-Type":"application/json"}).end(JSON.stringify({error:{code:"not_found",message:"no route"}}));return}
  if(mode==="fails"&&s.asked===1){res.writeHead(503,{"Content-Type":"application/json"}).end(JSON.stringify({error:{code:"busy",message:"try again"}}));return}
  // Codex is still being asked for two and a half seconds: the native tier has nothing for it.
  const done=Date.now()-s.started>2500
  res.setHeader("Content-Type","application/json")
  res.end(JSON.stringify({agents:[
    row("claude","Opus",["Opus","Sonnet","Haiku"],false,"native"),
    done?row("codex","Model Sol",["Model Sol","Model Astra Mini"],false,"agent",["high","xhigh","max"]):row("codex","",[],true,"native",["high","xhigh","max"]),
    row("antigravity","Pro",["Flash"],false,"native",["low","high"]),
    row("grok","",[],false,"native"),
  ]}))
}
const server=createServer(async(req,res)=>{
  const url=new URL(req.url,"http://fixture"),mode=new URL(req.headers.referer??"http://fixture/").search.slice(1)||"plain"
  if(url.pathname==="/"){res.setHeader("Content-Type","text/html");res.end(html);return}
  if(url.pathname==="/v1/model-catalog"){catalogAnswer(mode,res);return}
  if(url.pathname==="/v1/settings/default-models"){
    if(req.method==="POST"){let body="";for await(const c of req)body+=c;Object.assign(saved,JSON.parse(body||"{}"))}
    res.setHeader("Content-Type","application/json");res.end(JSON.stringify(saved));return
  }
  if(url.pathname.startsWith("/catalog/")){try{res.setHeader("Content-Type","application/json");res.end(await readFile(new URL(path.basename(url.pathname),catalogs)))}catch{res.writeHead(404).end()}return}
  const file=files.get(url.pathname)
  if(!file){res.writeHead(404).end();return}
  res.setHeader("Content-Type",types[path.extname(url.pathname)]??"text/javascript");res.end(file)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin="http://127.0.0.1:"+server.address().port
const browser=await browsers.chromium.launch({args:["--disable-features=OmniboxEverywhere"]})

// The open list lies wholly inside the section that holds the picker.
async function listInside(page,list,label){
  const seen=await page.evaluate(sel=>{
    const l=document.querySelector(sel),box=l?.closest("section")
    if(!l||!box)return null
    const a=l.getBoundingClientRect(),b=box.getBoundingClientRect()
    return {l:{top:a.top,bottom:a.bottom,left:a.left,right:a.right},b:{top:b.top,bottom:b.bottom,left:b.left,right:b.right},page:document.documentElement.scrollWidth<=innerWidth}
  },list)
  assert.ok(seen,`${label}: a list is open`)
  const {l,b}=seen
  assert.ok(l.left>=b.left-0.5&&l.right<=b.right+0.5&&l.top>=b.top-0.5&&l.bottom<=b.bottom+0.5,`${label}: list ${JSON.stringify(l)} inside ${JSON.stringify(b)}`)
  assert.ok(seen.page,`${label}: no horizontal page scroll`)
}
const optionLabels=page=>page.locator(".choice-menu-list [role=option] .choice-menu-text").allTextContents()

try{
  for(const [name,viewport] of [["1280",{width:1280,height:900}],["390",{width:390,height:844}]]){
    const touch=name==="390"
    const open=async mode=>{state.delete(mode);saved=fresh();const page=await browser.newPage({viewport,deviceScaleFactor:2,locale:"zh-TW",isMobile:touch,hasTouch:touch});await page.goto(origin+"/?"+mode);return page}

    // A refreshing answer: Codex waits inside its picker, then fills in without a reload.
    let page=await open("plain")
    const codexMenu=page.locator("#settings-codex-default-model")
    await codexMenu.waitFor()
    const waiting=page.locator("#settings [data-model-catalog=loading]")
    await waiting.first().waitFor()
    await page.screenshot({path:path.join(PICKERS_ARTIFACTS,`settings-refreshing-${name}.png`),fullPage:true})
    await codexMenu.click()
    assert.deepEqual(await optionLabels(page),["Agent 預設","model-retired"],"while Codex is asked, the default and the saved model are there")
    await listInside(page,".choice-menu-list","codex while refreshing")
    await page.keyboard.press("Escape")
    await waiting.first().waitFor({state:"detached",timeout:8000})
    await codexMenu.click()
    assert.deepEqual(await optionLabels(page),["Agent 預設（Model Sol）","Model Sol","Model Astra Mini","model-retired"],"the agent's answer arrives and the retired model stays")
    await listInside(page,".choice-menu-list","codex after refresh")
    await page.screenshot({path:path.join(PICKERS_ARTIFACTS,`settings-codex-open-${name}.png`),fullPage:true})
    await page.locator(".choice-menu-list [role=option]",{hasText:"Model Astra Mini"}).click()
    await page.locator("#settings-default-models .said",{hasText:/./}).waitFor()
    assert.equal(saved.codex_default_model,"model-astra-mini","a pick saves the catalog value")
    await page.locator("#settings-codex-default-effort").click()
    const efforts=await optionLabels(page)
    assert.deepEqual(efforts.slice(1),["high","xhigh","max"],"Codex efforts come from the catalog")
    await listInside(page,".choice-menu-list","codex effort")
    await page.keyboard.press("Escape")
    // The Board's launch row reads the same catalog.
    await page.locator("#board .work-launch-toggle").click()
    const board=await page.locator("#work-models-fixture [role=radio]").allTextContents()
    assert.deepEqual(board.slice(1),["Model Sol","Model Astra Mini"],"the Board row offers the same Codex models")
    await page.screenshot({path:path.join(PICKERS_ARTIFACTS,`board-row-${name}.png`),fullPage:true})
    await page.close()

    // A failure says so with Retry; Retry reads again and the list arrives.
    page=await open("fails")
    const failed=page.locator("#settings [data-model-catalog=failed]").first()
    await failed.waitFor()
    await page.screenshot({path:path.join(PICKERS_ARTIFACTS,`settings-failed-${name}.png`),fullPage:true})
    await failed.getByRole("button").click()
    await page.locator("#settings [data-model-catalog=failed]").first().waitFor({state:"detached",timeout:8000})
    await page.locator("#settings-claude-default-model").click()
    assert.deepEqual(await optionLabels(page),["Agent 預設（Opus）","Opus","Sonnet","Haiku"],"Retry recovers the list")
    await page.close()

    // An older daemon (no catalog route) offers only the agent default, with a note, and never spins.
    page=await open("older")
    const note=page.locator("#settings [data-model-catalog=unsupported]").first()
    await note.waitFor()
    assert.equal(await page.locator("#settings [data-model-catalog=loading]").count(),0,"no spinner for an older machine")
    await page.locator("#settings-claude-default-model").click()
    assert.deepEqual(await optionLabels(page),["Agent 預設"],"only the agent default")
    await page.screenshot({path:path.join(PICKERS_ARTIFACTS,`settings-older-${name}.png`),fullPage:true})
    await page.close()
    console.log(`pickers ${name}: refreshing, failed+retry, older: ok`)
  }
}finally{
  await browser.close()
  server.close()
}
