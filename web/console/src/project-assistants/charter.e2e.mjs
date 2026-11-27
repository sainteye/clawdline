// The team charter in Bots, in a real browser at 1280x800, 979x605 and 390x844,
// against the in-memory charter fixture (charter-fixture.ts), which answers the
// real /v1/project-assistant-charters route shapes through the Bot source:
// the roster row is present for a Project with a charter and absent without;
// a section edit saves as the next version, and a stale save rereads and says
// so while keeping the edit; a change request's proposal is shown as a diff and
// applied or dismissed; the history panel restores an old version as a new one;
// the settings tab creates a charter from the five-heading template.
// A section's title is its own field, so clearing the body keeps the heading;
// the page cannot be scrolled under the roster; a proposal the lead records
// seconds later appears without a reload, also in a background tab, and
// opening the charter again rereads it; 移除章程 asks once and removes it.
// The Bots' conflict reports: the roster row and a reminder count them, the
// side panel (a full-screen sheet on a phone) quotes both sentences, and each
// of 以章程為準, 改章程 and 先不管 does what it says.
// Playbooks (API 127, "?playbooks"), at 1280x800 and 390x844: the cards say who
// each is for and whether they read it; the detail reminds who has not; edit,
// reassign (a bottom sheet on a phone), delete, create, history and a playbook's
// conflict report each do what they say; an older machine shows no section.
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
  const value=await area.inputValue(),title=await page.getByRole("textbox",{name:"第 3 段標題"}).inputValue()
  check(`${name} editor holds the section body, its title apart`,()=>{assert.equal(value,"部署前先問。");assert.equal(title,"什麼要問你")})
  await area.fill("部署和刪除前先問。")
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
  await five.fill("寫清楚目標、驗收方式和截止時間。")
  await fixture(page,()=>window.charterFixture.studio.saveElsewhere("project",window.charterFixture.studio.text("project").replace("用交辦卡","一律用交辦卡")))
  if(narrow)await page.locator(".pa-charter-editor-bar").getByRole("button",{name:"儲存",exact:true}).click()
  else await page.getByRole("button",{name:"儲存為第 5 版"}).click()
  await page.getByText("你編輯時有人存了第 5 版").waitFor()
  await shot(page,`conflict-${name}`)
  const kept=await five.inputValue()
  check(`${name} conflict keeps the edit`,()=>assert.equal(kept,"寫清楚目標、驗收方式和截止時間。"))
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

// The roster column's left edge, and where the page's header pill starts.
const edges=page=>page.evaluate(()=>({scroll:document.querySelector(".pa-charter")?.scrollLeft,pill:document.querySelector(".pa-charter-pill")?.getBoundingClientRect().left,main:document.querySelector(".pa-main")?.getBoundingClientRect().left}))
async function headingsAndLayout(page,name,narrow){
  await open(page)
  await page.locator(".pa-charter-row").click()
  await page.locator(".pa-charter-section").first().waitFor()
  // A focus or scrollIntoView toward the right edge once slid the page under the roster.
  const reach=()=>page.evaluate(()=>{for(const b of [...document.querySelectorAll(".pa-charter button")].reverse())b.scrollIntoView({inline:"center",block:"nearest"})})
  await reach()
  const opened=await edges(page)
  check(`${name} opened page stays beside the roster`,()=>{assert.equal(opened.scroll,0);assert.ok(opened.pill>=opened.main,JSON.stringify(opened))})
  await page.getByRole("button",{name:"編輯第 2 段"}).click()
  const body=page.getByRole("textbox",{name:"第 2 段內容"})
  await body.fill("")
  await body.fill("先開卡，派工前先確認範圍。")
  if(narrow)await page.locator(".pa-charter-editor-bar").getByRole("button",{name:"儲存",exact:true}).click()
  else{await reach();await page.getByRole("button",{name:"儲存為第 4 版"}).click()}
  await page.getByText("已存成第 4 版。").waitFor()
  await reach()
  const saved=await edges(page)
  check(`${name} saved page stays beside the roster`,()=>{assert.equal(saved.scroll,0);assert.ok(saved.pill>=saved.main,JSON.stringify(saved))})
  await shot(page,`layout-${name}`)
  const text=await fixture(page,()=>window.charterFixture.studio.text("project"))
  check(`${name} clearing a body keeps its heading`,()=>assert.match(text,/## 一件事怎麼走\n\n先開卡，派工前先確認範圍。\n\n## 什麼要問你/))
  // A new title renames the section; an empty one keeps it.
  await page.getByRole("button",{name:"編輯第 3 段"}).click()
  await page.getByRole("textbox",{name:"第 3 段標題"}).fill("")
  await page.getByRole("textbox",{name:"第 3 段內容"}).fill("部署前一定要問。")
  if(narrow)await page.locator(".pa-charter-editor-bar").getByRole("button",{name:"儲存",exact:true}).click()
  else await page.getByRole("button",{name:"儲存為第 5 版"}).click()
  await page.getByText("已存成第 5 版。").waitFor()
  const kept=await fixture(page,()=>window.charterFixture.studio.text("project"))
  check(`${name} an empty title keeps the heading`,()=>assert.match(kept,/## 什麼要問你\n\n部署前一定要問。/))
}

// The lead records its proposal seconds after the request, as on a real machine.
async function lateProposal(page,name){
  await open(page)
  await page.locator(".pa-charter-row").click()
  await page.locator(".pa-charter-section").first().waitFor()
  await fixture(page,()=>window.charterFixture.studio.holdProposals(true))
  const composer=page.getByRole("textbox",{name:"要求修改章程"})
  await composer.fill("交辦卡要寫截止時間")
  await composer.press("Enter")
  await page.getByText("主助理正在寫提案，好了會馬上出現在這裡。").waitFor()
  const before=await page.locator(".pa-charter-proposal").count()
  check(`${name} nothing to show yet`,()=>assert.equal(before,0))
  // The person switched to another tab meanwhile.
  await page.evaluate(()=>{Object.defineProperty(document,"visibilityState",{configurable:true,get:()=>"hidden"});document.dispatchEvent(new Event("visibilitychange"))})
  await page.waitForTimeout(700)
  await fixture(page,()=>window.charterFixture.studio.releaseProposal("project"))
  const started=Date.now()
  await page.locator(".pa-charter-proposal").waitFor({timeout:3500})
  check(`${name} the late proposal appears within one fast read`,()=>assert.ok(Date.now()-started<3500))
  const waiting=await page.getByText("主助理正在寫提案").count()
  check(`${name} waiting line gone once the card is shown`,()=>assert.equal(waiting,0))
  await shot(page,`late-proposal-${name}`)
  await page.getByRole("button",{name:"不要",exact:true}).click()
  await page.getByText("已略過這個提案，章程沒變。").waitFor()
  // Back in the tab; a proposal recorded meanwhile shows when the charter is opened again.
  await page.evaluate(()=>{delete document.visibilityState})
  await fixture(page,()=>{window.charterFixture.studio.holdProposals(false);window.charterFixture.studio.propose("project","部署前先問。","部署和花錢前先問。","什麼要問你加上花錢")})
  const reopened=Date.now()
  if(await page.locator(".pa-charter-row").isVisible())await page.locator(".pa-charter-row").click()
  else{await page.locator(".pa-chat-back").click();await page.locator(".pa-charter-row").click()}
  await page.locator(".pa-charter-proposal").waitFor({timeout:3000})
  check(`${name} opening the charter again rereads it`,()=>assert.ok(Date.now()-reopened<3000))
}

async function removal(page,name){
  await open(page)
  await page.locator(".pa-charter-row").click()
  await page.locator(".pa-charter-section").first().waitFor()
  await page.getByRole("button",{name:"移除章程"}).click()
  const confirm=page.getByRole("group",{name:"移除章程"})
  await confirm.waitFor()
  const words=await confirm.innerText()
  check(`${name} removal asks once and says what every Bot hears`,()=>{assert.match(words,/要移除「Clawdline」的團隊章程嗎？/);assert.match(words,/每位 Bot 下一則訊息會收到「已沒有團隊章程」/);assert.match(words,/版本紀錄會保留/)})
  await shot(page,`remove-confirm-${name}`)
  const small=await smallTargets(page,".pa-charter-remove-confirm")
  check(`${name} removal touch targets`,()=>assert.deepEqual(small,[]))
  await confirm.getByRole("button",{name:"移除",exact:true}).click()
  await page.locator(".pa-charter").waitFor({state:"detached"})
  const state=await fixture(page,()=>({version:window.charterFixture.studio.version("project"),body:window.charterFixture.studio.log.filter(r=>r.path.endsWith("/remove")).map(r=>r.body)}))
  check(`${name} removed at the version read`,()=>{assert.equal(state.version,0);assert.equal(state.body.length,1);assert.equal(state.body[0].expected_version,3)})
  if(name==="390")await page.locator(".pa-bot-row").first().waitFor()
  await page.waitForTimeout(300)
  const rows=await page.locator(".pa-charter-row").count()
  check(`${name} roster row gone after removal`,()=>assert.equal(rows,0))
  await shot(page,`removed-${name}`)
}

async function conflicts(page,name,narrow){
  await page.goto(origin+"/")
  await page.waitForFunction(()=>!!window.charterFixture&&!!window.botsFixture)
  await page.evaluate(()=>{
    const studio=window.charterFixture.studio
    studio.reportConflict("project",{bot_id:"bot-1",source:"workspace_file",path:"docs/playbook.md",bot_sentence:"部署：GitHub、app.clawdline.com、本機 dev 各自要有使用者授權。",charter_sentence:"部署前先問。",section:"什麼要問你"})
    studio.reportConflict("project",{bot_id:"bot-5",source:"instructions",bot_sentence:"直接改別人的檔案比較快。",charter_sentence:"用交辦卡，不要私下改別人的檔案。",section:"Bot 之間怎麼溝通"})
    studio.reportConflict("project",{bot_id:"bot-3",source:"memory",bot_sentence:"開卡可以省略。",charter_sentence:"先開卡，再派工，做完回報。",section:"一件事怎麼走"},3)
    window.botsFixture.open()
  })
  await page.locator(".pa-charter-row").first().waitFor()
  const row=await page.locator(".pa-charter-row").innerText()
  check(`${name} roster row counts the conflicts`,()=>assert.match(row,/團隊章程[\s\S]*3 個衝突/))
  await page.locator(".pa-charter-row").click()
  const banner=page.locator(".pa-charter-conflict-banner")
  await banner.waitFor()
  const bannerText=await banner.innerText()
  check(`${name} reminder text`,()=>assert.match(bannerText,/3 個和章程不一樣的地方[\s\S]*查看/))
  await shot(page,`conflicts-reminder-${name}`)
  await banner.click()
  const panel=page.locator(".pa-charter-conflicts")
  await panel.getByText("和章程不一樣的地方",{exact:true}).waitFor()
  await shot(page,`conflicts-panel-${name}`)
  const cards=panel.locator(".pa-charter-conflict")
  const count=await cards.count()
  check(`${name} three cards`,()=>assert.equal(count,3))
  const first=await cards.nth(0).innerText()
  check(`${name} card quotes the Bot and the charter`,()=>{assert.match(first,/Documentation · .* 回報/);assert.match(first,/它的記憶[\s\S]*開卡可以省略。[\s\S]*章程第 2 段[\s\S]*先開卡，再派工，做完回報。/)})
  const file=await cards.nth(2).innerText()
  check(`${name} a playbook card names its file and the section number`,()=>assert.match(file,/Code review[\s\S]*它的 playbook · docs\/playbook.md[\s\S]*章程第 3 段[\s\S]*部署前先問。/))
  const footer=await panel.innerText()
  check(`${name} others and judgment`,()=>{assert.match(footer,/其他 3 位 Bot 沒有回報不一致/);assert.match(footer,/這是每位 Bot 自己的判斷，可能有遺漏。/);assert.match(footer,/章程優先，但由你決定要不要改。/)})
  const box=await panel.boundingBox(),viewport=page.viewportSize()
  if(narrow)check(`${name} the panel is a full-screen sheet`,()=>assert.ok(box&&Math.abs(box.width-viewport.width)<1&&box.x<1,JSON.stringify(box)))
  else check(`${name} the panel is about 380px at the right`,()=>assert.ok(box&&Math.abs(box.width-380)<=24&&Math.abs(box.x+box.width-viewport.width)<1,JSON.stringify(box)))
  const small=await smallTargets(page,".pa-charter-conflicts")
  check(`${name} conflict panel touch targets`,()=>assert.deepEqual(small,[]))
  const width=await page.evaluate(()=>({doc:document.documentElement.scrollWidth,inner:innerWidth}))
  check(`${name} no sideways scroll with the panel`,()=>assert.ok(width.doc<=width.inner,JSON.stringify(width)))
  // 以章程為準 on a playbook file goes to the Bot; on instructions the daemon fixes it.
  await cards.nth(2).getByRole("button",{name:"以章程為準，改掉它的那一句"}).click()
  await panel.getByText("已交給 Code review 修改").waitFor()
  await cards.nth(0).getByRole("button",{name:"以章程為準，改掉它的那一句"}).click()
  await panel.getByText("已改成和章程一致").waitFor()
  // 先不管 removes the report from the list.
  await cards.nth(0).getByRole("button",{name:"先不管"}).click()
  await page.getByText("這一項先不管。").first().waitFor()
  const after=await cards.count()
  check(`${name} a dismissed report leaves the list`,()=>assert.equal(after,2))
  await shot(page,`conflicts-handled-${name}`)
  const reminder=await page.locator(".pa-charter-conflict-banner").count()
  check(`${name} no reminder without open reports`,()=>assert.equal(reminder,0))
  // 改章程 opens the request box with both quotes.
  await page.evaluate(()=>window.charterFixture.studio.reportConflict("project",{bot_id:"bot-2",source:"memory",bot_sentence:"交辦卡不用寫驗收方式。",charter_sentence:"寫清楚目標和驗收方式。",section:"交辦卡"}))
  await page.waitForFunction(()=>!!document.querySelector(".pa-charter-conflict-banner")||document.querySelectorAll(".pa-charter-conflict[data-state=open]").length>0,null,{timeout:15000})
  if(!(await page.locator(".pa-charter-conflicts:not([hidden])").count()))await page.locator(".pa-charter-conflict-banner").click()
  await panel.locator('.pa-charter-conflict[data-state="open"]').getByRole("button",{name:"改章程"}).click()
  await page.getByText("修改章程的請求已放進下方輸入框，確認後再送出。").first().waitFor()
  const request=await page.getByRole("textbox",{name:"要求修改章程"}).inputValue()
  check(`${name} the request names the Bot and both quotes`,()=>assert.match(request,/Interface design 的記憶寫「交辦卡不用寫驗收方式。」，但章程第 5 段寫「寫清楚目標和驗收方式。」。請改章程/))
  await shot(page,`conflicts-change-request-${name}`)
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

// The charter's playbooks on a machine at API 127 ("?playbooks"); an older machine shows no section.
const playbookBodies=page=>fixture(page,()=>window.charterFixture.studio.log.filter(r=>r.path.endsWith("/playbook")||r.path.endsWith("/playbook-remove")).map(r=>r.body))
async function playbooks(page,name,narrow){
  await open(page)
  await page.locator(".pa-charter-row").click()
  await page.locator(".pa-charter-section").first().waitFor()
  const older=await page.locator(".pa-charter-playbooks").count()
  check(`${name} an older machine shows no playbook section`,()=>assert.equal(older,0))
  await page.goto(origin+"/?playbooks")
  await page.waitForFunction(()=>!!window.charterFixture&&!!window.botsFixture)
  await page.evaluate(()=>{window.charterFixture.studio.markRead("project","bot-3");window.botsFixture.open()})
  await page.locator(".pa-charter-row").click()
  const section=page.locator(".pa-charter-playbooks")
  await section.locator(".pa-charter-playbook-card").first().waitFor()
  const head=await section.locator(".pa-charter-playbooks-head").innerText()
  check(`${name} section heading, hint and add button`,()=>{assert.match(head,/Playbook[\s\S]*每份只交給指派的 Bot；章程優先於 Playbook[\s\S]*＋ 新增 Playbook/)})
  const cards=section.locator(".pa-charter-playbook-card")
  const texts=await cards.allInnerTexts()
  check(`${name} three cards with name, first sentence, Bots and state`,()=>{
    assert.equal(texts.length,3)
    assert.match(texts[0],/品質驗收[\s\S]*每次交付前，先跑完和修改相關的測試。[\s\S]*Code review[\s\S]*Interface design[\s\S]*○ 還沒讀到這次修改/)
    assert.match(texts[1],/發版步驟[\s\S]*標記 commit。[\s\S]*Documentation[\s\S]*● 已讀到最新/)
    assert.match(texts[2],/文件寫法[\s\S]*先寫結論，再寫原因。[\s\S]*沒有指派/)})
  const avatars=await cards.nth(0).locator(".pa-charter-playbook-bot svg").count()
  check(`${name} cards use the Bots' line avatars`,()=>assert.equal(avatars,2))
  const borders=await cards.evaluateAll(list=>list.map(c=>getComputedStyle(c).borderTopColor))
  const unreadText=await cards.nth(0).locator(".pa-charter-playbook-state").evaluate(n=>getComputedStyle(n).color)
  check(`${name} an unread card has the amber border and text`,()=>{assert.equal(borders[0],"rgb(107, 90, 51)");assert.notEqual(borders[1],borders[0]);assert.equal(unreadText,"rgb(227, 149, 103)")})
  const tops=await cards.evaluateAll(list=>list.map(c=>Math.round(c.getBoundingClientRect().top)))
  if(narrow)check(`${name} one column on a phone`,()=>assert.ok(tops[1]>tops[0]&&tops[2]>tops[1],JSON.stringify(tops)))
  else check(`${name} two columns on a desktop`,()=>assert.ok(tops[0]===tops[1]&&tops[2]>tops[0],JSON.stringify(tops)))
  const status=await page.locator(".pa-charter-status p").innerText()
  check(`${name} header status counts each Bot at its own delivery version`,()=>assert.equal(status,"4 位 Bot 照第 6 版；Code review、Interface design 下一則訊息才換新版"))
  const rosterLine=await page.locator(".pa-charter-row").innerText()
  check(`${name} roster line counts playbook readers`,()=>assert.match(rosterLine,/第 6 版 · 4\/6 位 Bot 已換新版/))
  await section.scrollIntoViewIfNeeded()
  await shot(page,`playbooks-list-${name}`)
  const small=await smallTargets(page,".pa-charter-playbooks")
  check(`${name} playbook section touch targets`,()=>assert.deepEqual(small,[]))
  // The detail: the reminder names who has not read it and why.
  await fixture(page,()=>window.charterFixture.studio.setRunning("project","bot-2",true))
  await cards.nth(0).click()
  const panel=page.locator(".pa-charter-playbook")
  await panel.getByText("交給",{exact:true}).waitFor()
  await panel.getByText("它正在工作").waitFor()
  const reminder=await panel.locator(".pa-charter-playbook-reminder").innerText()
  check(`${name} unread reminder`,()=>{assert.match(reminder,/Interface design 還沒讀到這次修改；它正在工作，下一輪開始時會讀到/);assert.match(reminder,/Code review 還沒讀到這次修改；下一則訊息開始時會讀到/)})
  const rows=await panel.locator(".pa-charter-playbook-reader").allInnerTexts()
  check(`${name} 交給 rows show the version read and working or idle`,()=>{assert.equal(rows.length,2);assert.match(rows[0],/Code review[\s\S]*○ 讀到第 3 版[\s\S]*閒置/);assert.match(rows[1],/Interface design[\s\S]*○ 讀到第 3 版[\s\S]*工作中/)})
  const listed=await panel.locator(".pa-charter-playbook-text li").count()
  check(`${name} content rendered as Markdown`,()=>assert.equal(listed,2))
  const box=await panel.boundingBox(),viewport=page.viewportSize()
  if(narrow)check(`${name} the playbook is a full-screen page`,()=>assert.ok(box&&Math.abs(box.width-viewport.width)<1&&box.x<1,JSON.stringify(box)))
  else check(`${name} the playbook panel is about 380px at the right`,()=>assert.ok(box&&Math.abs(box.width-380)<=24&&Math.abs(box.x+box.width-viewport.width)<1,JSON.stringify(box)))
  await shot(page,`playbook-detail-${name}`)
  const panelSmall=await smallTargets(page,".pa-charter-playbook")
  check(`${name} playbook panel touch targets`,()=>assert.deepEqual(panelSmall,[]))
  const width=await page.evaluate(()=>({doc:document.documentElement.scrollWidth,inner:innerWidth}))
  check(`${name} no sideways scroll with the playbook open`,()=>assert.ok(width.doc<=width.inner,JSON.stringify(width)))
  // Reassign: Documentation gets it, Code review no longer does.
  await panel.getByRole("button",{name:"更改交給誰"}).click()
  const sheet=panel.locator(".pa-charter-assign")
  await sheet.waitFor()
  await sheet.getByRole("checkbox",{name:"Documentation"}).check()
  await sheet.getByRole("checkbox",{name:"Code review"}).uncheck()
  const after=await sheet.locator(".pa-charter-after").innerText()
  check(`${name} reassign says who reads it next and who is told`,()=>{assert.match(after,/下一輪會讀到：Documentation/);assert.match(after,/下一輪會被告知不再適用：Code review/)})
  const offered=await sheet.getByRole("checkbox").count()
  check(`${name} every visible Bot is offered`,()=>assert.equal(offered,6))
  const sheetBox=await sheet.boundingBox()
  if(narrow)check(`${name} reassign is a bottom sheet`,()=>assert.ok(sheetBox&&Math.abs(sheetBox.y+sheetBox.height-viewport.height)<1&&Math.abs(sheetBox.width-viewport.width)<1,JSON.stringify(sheetBox)))
  else check(`${name} reassign expands inside the panel`,()=>assert.ok(sheetBox&&sheetBox.x>=box.x&&sheetBox.x+sheetBox.width<=box.x+box.width,JSON.stringify(sheetBox)))
  await shot(page,`playbook-assign-${name}`)
  await sheet.getByRole("button",{name:"儲存為第 7 版"}).click()
  await panel.getByText("已存成第 7 版。").waitFor()
  const assigned=await fixture(page,()=>window.charterFixture.studio.version("project"))
  const bodies=await playbookBodies(page)
  check(`${name} reassignment saved with its reason`,()=>{assert.equal(assigned,7);assert.deepEqual(bodies.at(-1).playbook.bot_ids,["bot-2","bot-3"]);assert.equal(bodies.at(-1).reason,"更改 Playbook『品質驗收』交給誰")})
  // Edit: name and text, and who reads it after saving.
  await panel.getByRole("button",{name:"編輯 Playbook"}).click()
  const editor=page.locator(".pa-charter-playbook-editor")
  await editor.waitFor()
  const title=await editor.getByRole("textbox",{name:"Playbook 名稱"}).inputValue()
  check(`${name} editor holds the name`,()=>assert.equal(title,"品質驗收"))
  await editor.getByRole("textbox",{name:"Playbook 內容"}).fill("每次交付前，先跑完相關測試。\n\n- 列出驗證過的項目\n- 畫面修改附上截圖\n- 寫下沒驗到的部分")
  const editAfter=await editor.locator(".pa-charter-after").innerText()
  check(`${name} after-save names only the assigned Bots`,()=>{assert.match(editAfter,/下一則訊息就讀新版：Documentation/);assert.match(editAfter,/正在做事：Interface design/);assert.doesNotMatch(editAfter,/Code review/)})
  const editBox=await editor.boundingBox()
  if(narrow){
    check(`${name} phone edit is full screen`,()=>assert.ok(editBox&&editBox.x<=0.5&&editBox.y<=0.5&&editBox.width>=389,JSON.stringify(editBox)))
    const bar=await editor.locator(".pa-charter-editor-bar").innerText()
    check(`${name} phone edit top bar`,()=>assert.match(bar,/取消[\s\S]*編輯 Playbook[\s\S]*儲存/))
  }
  await shot(page,`playbook-edit-${name}`)
  if(narrow)await editor.locator(".pa-charter-editor-bar").getByRole("button",{name:"儲存",exact:true}).click()
  else await editor.getByRole("button",{name:"儲存為第 8 版"}).click()
  await panel.getByText("已存成第 8 版。").waitFor()
  const edited=await playbookBodies(page)
  check(`${name} edit saved with its reason`,()=>{assert.equal(edited.at(-1).reason,"編輯 Playbook『品質驗收』");assert.match(edited.at(-1).playbook.text,/寫下沒驗到的部分/)})
  // Delete: quiet at the bottom, confirmed once.
  await panel.getByRole("button",{name:"刪除這份 Playbook"}).click()
  const confirm=panel.getByRole("group",{name:"刪除這份 Playbook"})
  await confirm.waitFor()
  const words=await confirm.innerText()
  check(`${name} delete confirm says who is told`,()=>{assert.match(words,/要刪除 Playbook「品質驗收」嗎？/);assert.match(words,/Interface design、Documentation 下一則訊息會被告知這份 Playbook 不再適用/)})
  await confirm.scrollIntoViewIfNeeded()
  await shot(page,`playbook-delete-${name}`)
  await confirm.getByRole("button",{name:"刪除",exact:true}).click()
  await page.getByText("已刪除 Playbook「品質驗收」，存成第 9 版。").waitFor()
  const left=await cards.count(),hidden=await panel.isHidden()
  check(`${name} deleted: two cards left and the panel closed`,()=>{assert.equal(left,2);assert.ok(hidden)})
  // New: name, text and who gets it in one go.
  await section.getByRole("button",{name:"＋ 新增 Playbook"}).click()
  const fresh=page.locator(".pa-charter-playbook-editor")
  await fresh.waitFor()
  await fresh.getByRole("textbox",{name:"Playbook 名稱"}).fill("測試寫法")
  await fresh.getByRole("textbox",{name:"Playbook 內容"}).fill("先寫會失敗的測試，再修。")
  await fresh.getByRole("checkbox",{name:"Testing"}).check()
  const freshAfter=await fresh.locator(".pa-charter-after").innerText()
  check(`${name} new playbook after-save`,()=>assert.match(freshAfter,/下一則訊息就讀新版：Testing/))
  await shot(page,`playbook-new-${name}`)
  if(narrow)await fresh.locator(".pa-charter-editor-bar").getByRole("button",{name:"儲存",exact:true}).click()
  else await fresh.getByRole("button",{name:"儲存為第 10 版"}).click()
  await panel.getByRole("heading",{name:"測試寫法"}).waitFor()
  const created=await playbookBodies(page)
  check(`${name} new playbook saved in one write with its reason`,()=>{assert.deepEqual(created.at(-1).playbook,{title:"測試寫法",text:"先寫會失敗的測試，再修。",bot_ids:["bot-5"]});assert.equal(created.at(-1).reason,"新增 Playbook『測試寫法』")})
  // History: the playbook writes are versions, diffed per playbook.
  await panel.getByRole("button",{name:"關閉"}).click()
  await page.getByRole("button",{name:"版本紀錄"}).click()
  const history=page.locator(".pa-charter-history")
  await history.getByText("第 10 版改了什麼").waitFor()
  const versions=await history.locator(".pa-charter-versions button").allInnerTexts()
  check(`${name} history lists the playbook writes with their reasons`,()=>{assert.match(versions[0],/第 10 版[\s\S]*新增 Playbook『測試寫法』/);assert.match(versions[2],/第 8 版[\s\S]*編輯 Playbook『品質驗收』/);assert.match(versions[3],/第 7 版[\s\S]*更改 Playbook『品質驗收』交給誰/)})
  const added=await history.locator(".pa-charter-playbook-diff").allInnerTexts()
  check(`${name} history diff of a new playbook`,()=>{assert.equal(added.length,1);assert.match(added[0],/新增 Playbook『測試寫法』[\s\S]*交給：Testing[\s\S]*先寫會失敗的測試/)})
  await history.getByRole("button",{name:/^第 9 版/}).click()
  await history.getByText("第 9 版改了什麼").waitFor()
  await history.getByText("刪除 Playbook『品質驗收』").waitFor()
  await history.getByRole("button",{name:/^第 8 版/}).click()
  await history.getByText("第 8 版改了什麼").waitFor()
  await history.locator(".pa-charter-playbook-diff").getByText("寫下沒驗到的部分").waitFor()
  const restoreHint=await history.locator(".pa-charter-history-diff").innerText()
  check(`${name} restore says playbooks come back too`,()=>assert.match(restoreHint,/還原成第 8 版[\s\S]*那一版的 Playbook 和交給誰也會一起還原/))
  await history.locator(".pa-charter-playbook-diff").scrollIntoViewIfNeeded()
  await shot(page,`playbook-history-${name}`)
  await history.getByRole("button",{name:"關閉版本紀錄"}).click()
  // A report from a playbook names the playbook.
  // The seeded 發版步驟 is the fixture's second playbook id.
  await page.evaluate(()=>{window.charterFixture.studio.reportConflict("project",{bot_id:"bot-3",source:"playbook",path:"pb-0000000000000002",bot_sentence:"部署不用問。",charter_sentence:"部署前先問。",section:"什麼要問你"})})
  await page.locator(".pa-charter-conflict-banner").waitFor({timeout:15000})
  await page.locator(".pa-charter-conflict-banner").click()
  const report=await page.locator(".pa-charter-conflicts .pa-charter-conflict").first().innerText()
  check(`${name} a playbook report names the playbook`,()=>assert.match(report,/章程 Playbook · 發版步驟[\s\S]*部署不用問。/))
  await shot(page,`playbook-conflict-${name}`)
}

try{
  for(const [name,viewport] of [["1280",{width:1280,height:800}],["979x605",{width:979,height:605}],["390",{width:390,height:844}],["1440x900",{width:1440,height:900}]]){
    const narrow=name==="390"
    // 1440x900 is the conflict panel's design size; the other screens are covered above.
    // CHARTER_ONLY=<run name> runs one check, at every size it belongs to.
    const runs=name==="1440x900"?[conflicts]:name==="979x605"?[rosterAndEdit,headingsAndLayout,proposals,lateProposal,history,removal,settings,conflicts]:[rosterAndEdit,headingsAndLayout,proposals,lateProposal,history,removal,settings,conflicts,playbooks]
    for(const run of runs.filter(r=>!process.env.CHARTER_ONLY||r.name===process.env.CHARTER_ONLY)){
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
