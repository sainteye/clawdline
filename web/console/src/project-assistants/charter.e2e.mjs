// The team charter in Bots, in a real browser at 1280x800, 979x605 and 390x844,
// against the in-memory charter fixture (charter-fixture.ts), which answers the
// real /v1/project-assistant-charters route shapes through the Bot source:
// the roster row is present for a Project with a charter and absent without;
// a section edit saves as the next version, and a stale save rereads and says
// so while keeping the edit; a change request's proposal is shown as a diff and
// applied or dismissed; the history panel restores an old version as a new one;
// the settings tab creates a charter from the five-heading template.
// PLAYWRIGHT_MODULE=<playwright index.mjs> CHARTER_ARTIFACTS=<dir> node charter.e2e.mjs
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile} from "node:fs/promises"
import path from "node:path"
import {pathToFileURL} from "node:url"
import {build} from "esbuild"

const {PLAYWRIGHT_MODULE,CHARTER_ARTIFACTS}=process.env
assert.ok(PLAYWRIGHT_MODULE&&CHARTER_ARTIFACTS,"Use an existing Playwright installation and a task-owned evidence directory")
await mkdir(CHARTER_ARTIFACTS,{recursive:true})
const browsers=await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
const out=await build({entryPoints:[new URL("charter.fixture.tsx",import.meta.url).pathname],outfile:"charter-fixture.js",write:false,bundle:true,format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(out.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const html=`<!doctype html><html lang="zh-Hant"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"><title>Charter fixture</title><link rel="stylesheet" href="/charter-fixture.css"><style>html,body,#fixture{margin:0;height:100%;background:#080808;color:#fafafa;font-family:-apple-system,BlinkMacSystemFont,sans-serif}#fixture{height:100dvh}#voice-owner{position:fixed;right:8px;top:0;font-size:9px}</style></head><body><div id="fixture"></div><script type="module">import {mountCharterFixture} from '/charter-fixture.js';mountCharterFixture();</script></body></html>`
const catalogs=new URL("../../public/catalogs/",import.meta.url)
const server=createServer(async(req,res)=>{
  const pathname=new URL(req.url,"http://fixture").pathname
  if(pathname==="/"){res.setHeader("Content-Type","text/html");res.end(html);return}
  if(pathname.startsWith("/catalog/")){try{res.setHeader("Content-Type","application/json");res.end(await readFile(new URL(path.basename(pathname),catalogs)))}catch{res.writeHead(404).end()}return}
  const file=files.get(pathname)
  if(!file){res.writeHead(404).end();return}
  res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript");res.end(file)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin="http://127.0.0.1:"+server.address().port
const browser=await browsers.chromium.launch({args:["--disable-features=OmniboxEverywhere"]})
const failures=[]
const check=(label,fn)=>{try{fn()}catch(error){failures.push(`${label}: ${process.env.CHARTER_DEBUG?error.message.slice(0,600):error.message.split("\n")[0]}`)}}
const shot=(page,name)=>page.screenshot({path:path.join(CHARTER_ARTIFACTS,`${name}.png`)})
const fixture=(page,expr)=>page.evaluate(expr)

async function open(page){
  await page.goto(origin+"/")
  await page.waitForFunction(()=>!!window.charterFixture&&!!window.botsFixture)
  await page.evaluate(()=>window.botsFixture.open())
  await page.locator(".pa-bot-row").first().waitFor()
}
// Every control a touch reaches is at least 44px both ways.
async function smallTargets(page,scope){
  return page.locator(scope).evaluateAll(nodes=>nodes.flatMap(node=>[...node.querySelectorAll("button")].filter(b=>b.offsetParent).map(b=>{const r=b.getBoundingClientRect();return {label:b.getAttribute("aria-label")||b.textContent.trim(),w:Math.round(r.width),h:Math.round(r.height)}}).filter(r=>r.w<44||r.h<44)))
}

async function rosterAndEdit(page,name,narrow){
  await open(page)
  await page.locator(".pa-charter-row").first().waitFor()
  await shot(page,`roster-${name}`)
  const rows=await page.locator(".pa-charter-row").count()
  check(`${name} one charter row (studio has one, notebook none)`,()=>assert.equal(rows,1))
  const rowText=await page.locator(".pa-charter-row").innerText()
  check(`${name} charter row subtitle`,()=>assert.match(rowText,/團隊章程[\s\S]*第 3 版 · 6 位 Bot 都照這版/))
  const first=await page.locator('section.pa-project-group[data-roster-project]').evaluateAll(sections=>sections.map(s=>s.querySelector(".pa-roster-entry")?.classList.contains("pa-charter-entry")))
  check(`${name} charter row is the first row of its Project`,()=>assert.ok(first.includes(true),JSON.stringify(first)))
  await page.locator(".pa-charter-row").click()
  await page.locator(".pa-charter-section").first().waitFor()
  await shot(page,`page-${name}`)
  const pill=await page.locator(".pa-charter-pill").innerText()
  check(`${name} header pill`,()=>assert.equal(pill.trim(),"團隊章程 · Clawdline · 第 3 版"))
  const status=await page.locator(".pa-charter-status p").innerText()
  check(`${name} status line`,()=>assert.equal(status,"6 位 Bot 都照第 3 版"))
  const sections=await page.locator(".pa-charter-section").count()
  check(`${name} five section cards`,()=>assert.equal(sections,5))
  if(narrow){
    const word=await page.getByRole("button",{name:"編輯第 1 段"}).innerText()
    check(`${name} phone edit button says 改`,()=>assert.equal(word.trim(),"改"))
    const width=await page.evaluate(()=>({doc:document.documentElement.scrollWidth,inner:innerWidth}))
    check(`${name} no sideways scroll`,()=>assert.ok(width.doc<=width.inner,JSON.stringify(width)))
  }
  const small=await smallTargets(page,".pa-charter-main")
  check(`${name} charter page touch targets`,()=>assert.deepEqual(small,[]))
  // A Bot running now waits for its turn to end; the others read it next.
  await fixture(page,()=>window.charterFixture.studio.setRunning("project","bot-2",true))
  await page.getByRole("button",{name:"編輯第 3 段"}).click()
  const area=page.getByRole("textbox",{name:"第 3 段內容"})
  await area.waitFor()
  const value=await area.inputValue()
  check(`${name} editor holds the section text`,()=>assert.equal(value,"## 什麼要問你\n\n部署前先問。"))
  await area.fill("## 什麼要問你\n\n部署和刪除前先問。")
  const editor=page.locator(".pa-charter-editor")
  await editor.getByText("正在做事").waitFor()
  const after=await editor.innerText()
  check(`${name} after-save box names who reads it next and who is running`,()=>{assert.match(after,/儲存後/);assert.match(after,/下一則訊息就讀新版：Project assistant、Code review/);assert.match(after,/正在做事：Interface design。這一輪做完才換新版，不會被打斷/)})
  check(`${name} edit hint`,()=>assert.match(after,/直接寫白話就好，不用照格式/))
  await shot(page,`edit-${name}`)
  if(narrow){
    const box=await editor.boundingBox()
    check(`${name} phone edit is full screen`,()=>assert.ok(box&&box.x<=0.5&&box.y<=0.5&&box.width>=389,JSON.stringify(box)))
    const bar=await page.locator(".pa-charter-editor-bar").innerText()
    check(`${name} phone edit top bar`,()=>assert.match(bar,/取消[\s\S]*儲存/))
    await page.locator(".pa-charter-editor-bar").getByRole("button",{name:"儲存",exact:true}).click()
  }else await page.getByRole("button",{name:"儲存為第 4 版"}).click()
  await page.locator(".pa-charter-notice").waitFor()
  const saved=await page.locator(".pa-charter-notice").innerText()
  check(`${name} saved as version 4`,()=>assert.equal(saved,"已存成第 4 版。"))
  const text=await fixture(page,()=>window.charterFixture.studio.text("project"))
  check(`${name} whole document saved with only that section changed`,()=>assert.match(text,/## 一件事怎麼走\n\n先開卡，再派工，做完回報。\n\n## 什麼要問你\n\n部署和刪除前先問。\n\n## Bot 之間怎麼溝通/))
  const log=await fixture(page,()=>window.charterFixture.studio.log.filter(r=>r.method==="PUT").map(r=>r.body))
  check(`${name} save names the version it read`,()=>assert.equal(log.at(-1)?.expected_version,3))
  // A stale save: another device saved meanwhile.
  await page.getByRole("button",{name:"編輯第 5 段"}).click()
  const five=page.getByRole("textbox",{name:"第 5 段內容"})
  await five.fill("## 交辦卡\n\n寫清楚目標、驗收方式和截止時間。")
  await fixture(page,()=>window.charterFixture.studio.saveElsewhere("project",window.charterFixture.studio.text("project").replace("用交辦卡","一律用交辦卡")))
  if(narrow)await page.locator(".pa-charter-editor-bar").getByRole("button",{name:"儲存",exact:true}).click()
  else await page.getByRole("button",{name:"儲存為第 5 版"}).click()
  await page.getByText("你編輯時有人存了第 5 版").waitFor()
  await shot(page,`conflict-${name}`)
  const kept=await five.inputValue()
  check(`${name} conflict keeps the edit`,()=>assert.equal(kept,"## 交辦卡\n\n寫清楚目標、驗收方式和截止時間。"))
  const pill5=await page.locator(".pa-charter-pill").innerText()
  check(`${name} conflict reread the charter`,()=>assert.match(pill5,/第 5 版/))
  if(narrow)await page.locator(".pa-charter-editor-bar").getByRole("button",{name:"儲存",exact:true}).click()
  else await page.getByRole("button",{name:"儲存為第 6 版"}).click()
  await page.getByText("已存成第 6 版。").waitFor()
  const merged=await fixture(page,()=>window.charterFixture.studio.text("project"))
  check(`${name} second save keeps the other device's change`,()=>{assert.match(merged,/一律用交辦卡/);assert.match(merged,/截止時間/)})
}

async function proposals(page,name,narrow){
  await open(page)
  await page.locator(".pa-charter-row").click()
  await page.locator(".pa-charter-section").first().waitFor()
  const composer=page.getByRole("textbox",{name:"要求修改章程"})
  const placeholder=await composer.getAttribute("placeholder")
  check(`${name} composer placeholder`,()=>assert.equal(placeholder,"想改哪裡？直接打在這裡"))
  await composer.fill("交辦卡要寫截止時間")
  await composer.press("Enter")
  await page.locator(".pa-charter-proposal").waitFor()
  await shot(page,`proposal-${name}`)
  const card=await page.locator(".pa-charter-proposal").innerText()
  check(`${name} proposal names the lead and its explanation`,()=>{assert.match(card,/Project assistant 的修改提案/);assert.match(card,/交辦卡要寫截止時間/)})
  const added=await page.locator('.pa-charter-proposal .pa-charter-diff-line[data-kind="added"]').allInnerTexts()
  check(`${name} proposal shows the added line`,()=>assert.ok(added.some(t=>t.includes("- 交辦卡要寫截止時間")),JSON.stringify(added)))
  const colours=await page.locator('.pa-charter-proposal .pa-charter-diff-line[data-kind="added"]').first().evaluate(n=>getComputedStyle(n).backgroundColor)
  check(`${name} added lines have the green diff background`,()=>assert.equal(colours,"rgba(77, 139, 91, 0.23)"))
  const highlighted=await page.locator(".pa-charter-section[data-proposed]").count()
  check(`${name} affected section highlighted`,()=>assert.equal(highlighted,1))
  const unchanged=await fixture(page,()=>window.charterFixture.studio.version("project"))
  check(`${name} nothing changes before apply`,()=>assert.equal(unchanged,3))
  const actions=await page.locator(".pa-charter-proposal .pa-charter-actions").innerText()
  check(`${name} proposal buttons`,()=>assert.match(actions,/套用，存成第 4 版[\s\S]*不要[\s\S]*再改一下/))
  await page.getByRole("button",{name:"套用，存成第 4 版"}).click()
  await page.getByText("已套用，存成第 4 版。").waitFor()
  const applied=await fixture(page,()=>window.charterFixture.studio.text("project"))
  check(`${name} applied text`,()=>assert.match(applied,/- 交辦卡要寫截止時間/))
  // A second proposal, dismissed.
  await composer.fill("什麼要問你加上花錢")
  await composer.press("Enter")
  await page.locator(".pa-charter-proposal").waitFor()
  await page.getByRole("button",{name:"再改一下"}).click()
  await page.getByText("跟主助理說這個提案要怎麼改。").waitFor()
  const focused=await page.evaluate(()=>document.activeElement?.getAttribute("aria-label"))
  check(`${name} revise focuses the composer`,()=>assert.equal(focused,"要求修改章程"))
  await page.getByRole("button",{name:"不要",exact:true}).click()
  await page.getByText("已略過這個提案，章程沒變。").waitFor()
  await shot(page,`dismissed-${name}`)
  const left=await page.locator(".pa-charter-proposal").count(),version=await fixture(page,()=>window.charterFixture.studio.version("project"))
  check(`${name} dismissed leaves the charter`,()=>{assert.equal(left,0);assert.equal(version,4)})
  void narrow
}

async function history(page,name){
  await open(page)
  await page.locator(".pa-charter-row").click()
  await page.getByRole("button",{name:"版本紀錄"}).click()
  const panel=page.locator(".pa-charter-history")
  await panel.getByText("第 3 版改了什麼").waitFor()
  await shot(page,`history-${name}`)
  const versions=await panel.locator(".pa-charter-versions button").allInnerTexts()
  check(`${name} versions newest first with who and what`,()=>{assert.equal(versions.length,3);assert.match(versions[0],/第 3 版[\s\S]*目前[\s\S]*主助理的提案：交辦卡寫清楚驗收方式[\s\S]*主助理/);assert.match(versions[2],/第 1 版[\s\S]*從範本建立[\s\S]*範本/)})
  const box=await panel.boundingBox()
  check(`${name} history is a right-side panel inside the window`,()=>assert.ok(box&&box.x+box.width<=page.viewportSize().width+0.5&&box.width>=300,JSON.stringify(box)))
  await panel.getByRole("button",{name:/^第 2 版/}).click()
  await panel.getByText("第 2 版改了什麼").waitFor()
  const diff=await panel.locator(".pa-charter-diff-line[data-kind]").evaluateAll(lines=>lines.filter(l=>l.dataset.kind!=="same").map(l=>[l.dataset.kind,l.textContent]))
  check(`${name} history diff against the version before`,()=>assert.deepEqual(diff.map(([k])=>k),["removed","added"]))
  await panel.getByRole("button",{name:/^第 1 版/}).click()
  await panel.getByText("第一版").waitFor()
  await panel.getByRole("button",{name:"還原成第 1 版"}).click()
  await page.getByText("已還原：第 1 版的內容存成第 4 版。").waitFor()
  await shot(page,`restored-${name}`)
  const restored=await fixture(page,()=>({text:window.charterFixture.studio.text("project"),version:window.charterFixture.studio.version("project")}))
  check(`${name} restore is a new version with the old text`,()=>{assert.equal(restored.version,4);assert.match(restored.text,/先開卡，再派工。\n/)})
  const top=await page.locator(".pa-charter-history .pa-charter-versions button").first().innerText()
  check(`${name} restore listed`,()=>assert.match(top,/第 4 版[\s\S]*還原第 1 版的內容/))
}

async function settings(page,name){
  await open(page)
  await page.getByRole("button",{name:"清單選項"}).click()
  await page.getByRole("menuitem",{name:"Bots 設定"}).click()
  const dialog=page.getByRole("dialog")
  await dialog.waitFor()
  await dialog.getByRole("button",{name:"團隊章程",exact:true}).click()
  await dialog.getByText("打開章程").waitFor()
  const studio=await dialog.locator(".pa-bs-charters").innerText()
  check(`${name} settings row for a Project with a charter`,()=>assert.match(studio,/Clawdline[\s\S]*第 3 版 · 6 位 Bot[\s\S]*打開章程/))
  const note=await dialog.locator(".pa-bs-group .pa-bs-note").first().innerText()
  check(`${name} settings explanatory sentence`,()=>assert.match(note,/每個 Project 有一份團隊章程/))
  await dialog.getByRole("button",{name:"機器"}).click()
  await page.getByRole("option",{name:"notebook"}).click()
  await dialog.getByText("建立章程").waitFor()
  await shot(page,`settings-${name}`)
  const notebook=await dialog.locator(".pa-bs-charters").innerText()
  check(`${name} settings row for a Project without one`,()=>assert.match(notebook,/還沒有章程 · 6 位 Bot[\s\S]*建立章程/))
  await dialog.getByRole("button",{name:"建立章程: Clawdline"}).click()
  await page.locator(".pa-charter-section").first().waitFor()
  await shot(page,`created-${name}`)
  const headings=await page.locator(".pa-charter-section").evaluateAll(cards=>cards.map(c=>c.getAttribute("aria-label")))
  check(`${name} template headings`,()=>assert.deepEqual(headings,["誰負責什麼","一件事怎麼走","什麼要問你","Bot 之間怎麼溝通","交辦卡"]))
  const first=await page.locator(".pa-charter-section").first().innerText()
  check(`${name} template lists the Project's Bots`,()=>{for(const bot of ["Project assistant","Code review","Testing"])assert.match(first,new RegExp(bot))})
  const pill=await page.locator(".pa-charter-pill").innerText()
  check(`${name} created as version 1`,()=>assert.match(pill,/第 1 版/))
  const rows=await page.locator(".pa-charter-row").count()
  if(name!=="390")check(`${name} roster now shows both charters`,()=>assert.equal(rows,2))
}

try{
  for(const [name,viewport] of [["1280",{width:1280,height:800}],["979x605",{width:979,height:605}],["390",{width:390,height:844}]]){
    const narrow=name==="390"
    for(const run of [rosterAndEdit,proposals,history,settings]){
      const page=await browser.newPage({viewport,deviceScaleFactor:2,locale:"zh-TW",isMobile:narrow,hasTouch:narrow,reducedMotion:"reduce"})
      page.setDefaultTimeout(4000)
      const errors=[];page.on("pageerror",e=>errors.push(e.message))
      try{await run(page,name,narrow)}catch(error){failures.push(`${name} ${run.name}: ${error.message.split("\n")[0]}`);await shot(page,`failed-${run.name}-${name}`).catch(()=>{})}
      check(`${name} ${run.name} page errors`,()=>assert.deepEqual(errors,[]))
      await page.close()
    }
  }
}finally{await browser.close();server.close()}
for(const f of failures)console.log("FAIL",f)
if(failures.length){console.log(failures.length,"failed");process.exit(1)}
console.log("ok")
