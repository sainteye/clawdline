/** Free local browser harness. Serves only an in-memory bundle and the explicit synthetic HTML. */
import assert from "node:assert/strict"
import { createServer } from "node:http"
import { readFile,mkdir,writeFile } from "node:fs/promises"
import { fileURLToPath,pathToFileURL } from "node:url"
import path from "node:path"
import { build } from "esbuild"
const artifacts=process.env.BOTS_ARTIFACTS
if(!artifacts)throw new Error("Set BOTS_ARTIFACTS to a task-owned evidence directory")
await mkdir(artifacts,{recursive:true})
if(!process.env.PLAYWRIGHT_MODULE)throw new Error("Set PLAYWRIGHT_MODULE to an existing local Playwright index.mjs; this harness installs nothing")
const {chromium,webkit}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const dir=fileURLToPath(new URL(".",import.meta.url))
const fenceOverride=process.env.BOTS_CREATE_SOURCE,chatOverride=process.env.BOTS_CHAT_SOURCE
const plugins=fenceOverride?[{name:"captured-create-fence-baseline",setup(build){build.onLoad({filter:/\/BotCreate\.tsx$/},async args=>({contents:await readFile(fenceOverride,"utf8"),loader:"tsx",resolveDir:path.dirname(args.path)}))}}]:[]
if(chatOverride)plugins.push({name:"bounded-read-chat-baseline",setup(build){build.onLoad({filter:/\/Chat\.tsx$/},async args=>({contents:await readFile(chatOverride,"utf8"),loader:"tsx",resolveDir:path.dirname(args.path)}))}})
const bundle=await build({plugins,entryPoints:[path.join(dir,"fixtures.ts")],outfile:"project-assistants-fixture.js",bundle:true,write:false,format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const content=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.text]))
const html=await readFile(new URL("../../tools/fixtures/project-assistants.html",import.meta.url),"utf8")
if(process.env.BOTS_ATTACHMENTS_ONLY){
  content.set("/catalog/zh-Hant.json",await readFile(new URL("../../public/catalogs/zh-Hant.json",import.meta.url),"utf8"))
  const shellStyles=await Promise.all(["tokens","base"].map(name=>readFile(new URL("../legacy/"+name+".css",import.meta.url),"utf8")))
  const stylesheet="/project-assistants-fixture.css"
  content.set(stylesheet,[...shellStyles,content.get(stylesheet)].join("\n"))
}
// This matrix verifies injected metadata behavior; real transport/store acceptance is separate.
async function catalogChecks(page,origin,language,shot,log) {
  const zh=language!=="en",w=zh?{create:"新增 Bot",submit:"建立",cancel:"取消",name:"名稱",machine:"機器",agent:"Agent",instructions:"固定指示",more:"載入更多",retry:"重試",search:"搜尋 Bot 或 Project",message:"訊息"}:{create:"Create Bot",submit:"Create",cancel:"Cancel",name:"Name",machine:"Machine",agent:"Agent",instructions:"Standing instructions",more:"Load more",retry:"Try again",search:"Search Bot or Project",message:"Message"}
  const dialog=page.getByRole("dialog"),project=()=>dialog.getByRole("combobox",{name:"Project",exact:true}),submit=()=>dialog.getByRole("button",{name:w.submit,exact:true})
  const add=page.getByRole("button",{name:zh?"新增":"Add",exact:true})
  const allRequests=[]
  const fresh=async(options={})=>{
    if(page.url().startsWith(origin))allRequests.push(...await log())
    await page.goto(origin+"/?lang="+language)
    await page.evaluate(options=>{window.botsFixture.a.configureProjects(options);window.botsFixture.open()},options)
    await add.waitFor()
  }
  const open=async()=>{await add.click();await page.getByRole("menuitem",{name:w.create,exact:true}).click();await dialog.waitFor()}
  const loaded=async(count=101)=>{await page.waitForFunction(count=>document.querySelector('.pa-form-panel select[required]')?.options.length===count,count);assert.equal(await project().isDisabled(),false)}
  const fill=async()=>{await dialog.getByRole("textbox",{name:w.name,exact:true}).fill("Catalog Bot");await dialog.getByRole("textbox",{name:w.instructions,exact:true}).fill("Preserve these instructions")}
  const alert=async(code)=>dialog.getByRole("alert").filter({hasText:code}).waitFor()
  const reset=async()=>dialog.getByRole("button",{name:w.retry,exact:true}).click()
  const metadata=()=>log().then(rows=>rows.filter(row=>["create","send","stop","answer"].includes(row.method)))
  await fresh({count:101});await open();await loaded();await fill()
  assert.deepEqual(await dialog.getByRole("combobox",{name:w.agent,exact:true}).locator("option").allTextContents(),["Claude Code","Codex"])
  assert.equal(await page.evaluate(async()=>{const s=await window.botsFixture.a.status(new AbortController().signal);return s.sources[0].capabilities.every(c=>!c.read_only_enforced&&!c.native_memory_verified&&!!c.unavailable)}),true)
  assert.equal(await project().locator('option[value="project"]').textContent(),await project().locator('option[value="project-1"]').textContent(),"same labels retain independent IDs")
  await project().selectOption("project-1");await dialog.getByRole("combobox",{name:w.agent,exact:true}).selectOption("codex")
  assert.equal(await submit().isDisabled(),false)
  assert.equal((await metadata()).length,0,"metadata reads and selections start no model or mutation")
  const firstRequests=(await log()).filter(row=>row.method==="projects")
  assert.equal(firstRequests.every(row=>row.request.cursor===""),true,"no automatic page draining")
  await shot("catalog-first-100")
  await dialog.getByRole("button",{name:w.more,exact:true}).click();await loaded(102)
  assert.equal(await project().inputValue(),"project-1")
  await project().selectOption("project-100");await shot("catalog-page-101")
  await submit().click();await dialog.waitFor({state:"hidden"})
  const created=(await log()).filter(row=>row.method==="create")
  assert.equal(created.length,1);assert.equal(created[0].request.settings.target.project_id,"project-100");assert.equal(created[0].request.settings.agent,"codex")
  assert.equal((await log()).filter(row=>row.method==="send").length,0)
  await page.getByRole("searchbox",{name:w.search,exact:true}).fill("Catalog Bot")
  await page.getByRole("button",{name:/^Catalog Bot, [^,]+, studio(,|$)/}).click()
  await page.getByRole("textbox",{name:w.message,exact:true}).fill("First send cannot admit a provider")
  await page.locator(".pa-composer .pa-send").click()
  await page.locator(".pa-chat").getByRole("alert").filter({hasText:"adapter_not_found"}).waitFor()
  assert.equal((await log()).filter(row=>row.method==="send").length,1,"explicit first Send retains typed unavailable")
  await shot("catalog-provider-unavailable")

  await fresh({failure:"pa_offline"});await open();await alert("pa_offline");await fill()
  assert.equal(await submit().isDisabled(),true);assert.equal(await project().isDisabled(),true)
  await page.evaluate(()=>window.botsFixture.a.configureProjects({failure:null}));await reset();await loaded(2)
  assert.equal(await dialog.getByRole("textbox",{name:w.name,exact:true}).inputValue(),"Catalog Bot")
  assert.equal(await dialog.getByRole("textbox",{name:w.instructions,exact:true}).inputValue(),"Preserve these instructions")
  await shot("catalog-initial-retry")

  for(const failure of ["pa_cursor_expired","pa_capacity_full"]) {
    await fresh({count:101});await open();await loaded();await fill();await project().selectOption("project-1")
    await page.evaluate(failure=>window.botsFixture.a.configureProjects({failure}),failure)
    await dialog.getByRole("button",{name:w.more,exact:true}).click();await alert(failure)
    assert.equal(await submit().isDisabled(),true)
    await page.evaluate(()=>window.botsFixture.a.configureProjects({failure:null}));await reset();await loaded(failure==="pa_cursor_expired"?101:102)
    assert.equal(await project().inputValue(),"project-1")
    assert.equal(await dialog.getByRole("textbox",{name:w.name,exact:true}).inputValue(),"Catalog Bot")
    await shot("catalog-retry-"+failure)
  }
  for(const invalid of ["revision","cursor","foreign","duplicate"]) {
    await fresh({count:101});await open();await loaded();await fill();await project().selectOption("project")
    await page.evaluate(invalid=>window.botsFixture.a.configureProjects({invalid}),invalid)
    await dialog.getByRole("button",{name:w.more,exact:true}).click()
    await alert(invalid==="revision"?"pa_cursor_expired":invalid==="foreign"?"pa_scope_denied":"pa_invalid")
    assert.equal(await submit().isDisabled(),true)
    await shot("catalog-reject-"+invalid)
  }

  await fresh({count:101});await open();await loaded();await fill();await project().selectOption("project-1")
  await page.evaluate(()=>window.botsFixture.a.holdProjects())
  await dialog.getByRole("button",{name:w.more,exact:true}).click()
  await dialog.getByRole("combobox",{name:w.machine,exact:true}).selectOption("notebook");await loaded(2)
  assert.equal(await project().inputValue(),"")
  await page.evaluate(()=>window.botsFixture.a.releaseProjects());await page.waitForTimeout(30)
  assert.equal(await project().locator("option").count(),2,"late old page cannot replace the new source")
  assert.equal(await dialog.getByRole("textbox",{name:w.name,exact:true}).inputValue(),"Catalog Bot")
  assert.equal(await dialog.getByRole("textbox",{name:w.instructions,exact:true}).inputValue(),"Preserve these instructions")
  await shot("catalog-source-switch")

  await fresh();await page.evaluate(()=>window.botsFixture.a.holdProjects());await open()
  await dialog.getByRole("button",{name:w.cancel,exact:true}).click()
  await page.evaluate(()=>window.botsFixture.a.releaseProjects());await page.waitForTimeout(30)
  assert.equal(await dialog.count(),0);assert.equal((await metadata()).length,0)
  await open();await loaded(2);assert.equal(await dialog.getByRole("textbox",{name:w.name,exact:true}).inputValue(),"")

  await fresh();await page.evaluate(()=>window.botsFixture.a.setState("online","history_only"));await open();await loaded(2);await fill();await project().selectOption("project")
  assert.equal(await submit().isDisabled(),true,"read-only metadata never admits create")
  assert.equal((await metadata()).length,0);await shot("catalog-read-only")

  await fresh();await open();await loaded(2);await fill();await project().selectOption("project")
  await page.evaluate(()=>window.botsFixture.a.setState("online","history_only"));await submit().click();await alert("pa_read_only")
  assert.equal((await metadata()).length,0,"fresh status rejects a write after metadata was loaded")

  await fresh();await page.evaluate(()=>window.botsFixture.a.holdProjects());await open();await page.evaluate(()=>window.botsFixture.revoke());await page.evaluate(()=>window.botsFixture.a.releaseProjects());await page.waitForTimeout(30)
  assert.equal(await submit().isDisabled(),true);assert.equal(await project().locator("option").count(),1)
  assert.equal((await metadata()).length,0);await shot("catalog-revoked")

  for(const authority of ["revoke","revision"]) {
    await fresh();await open();await loaded(2);await fill();await project().selectOption("project")
    const statuses=(await log()).filter(row=>row.method==="status").length
    await page.evaluate(()=>window.botsFixture.a.holdStatus());await submit().click()
    await page.waitForFunction(n=>window.botsFixture.log.filter(row=>row.method==="status").length>n,statuses)
    await page.evaluate(authority=>{if(authority==="revoke")window.botsFixture.revokeBeforeRender();else window.botsFixture.replaceGrantBeforeRender();window.botsFixture.a.releaseStatus()},authority)
    await alert("pa_revoked")
    assert.equal((await log()).filter(row=>row.method==="create").length,0,`effect fence after delayed status: ${authority}`)
    await shot("catalog-status-effect-fence-"+authority)
  }

  await fresh();await open();await loaded(2);await fill();await project().selectOption("project")
  await page.evaluate(()=>window.botsFixture.a.unknownCreate(true));await submit().click();await alert("pa_outcome_unknown")
  assert.equal(await dialog.getByRole("textbox",{name:w.name,exact:true}).isDisabled(),true)
  assert.equal((await log()).filter(row=>row.method==="create").length,1,"unknown create is never automatically repeated")
  await shot("catalog-create-unknown")
  await page.evaluate(()=>window.botsFixture.a.setState("online","history_only"));await reset();await alert("pa_read_only")
  assert.equal(await dialog.getByRole("textbox",{name:w.name,exact:true}).isDisabled(),true,"preflight refusal cannot unlock an earlier unknown create")
  assert.equal((await log()).filter(row=>row.method==="create").length,1)
  await page.evaluate(()=>{window.botsFixture.a.setState("online","interactive");window.botsFixture.a.unknownCreate(false)});await reset();await dialog.waitFor({state:"hidden"})
  const attempts=(await log()).filter(row=>row.method==="create")
  assert.equal(attempts.length,2);assert.deepEqual(attempts[0].request,attempts[1].request,"explicit retry uses identical idempotency and settings")
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
  await fresh();await open();await loaded(2);await fill();await project().selectOption("project")
  await page.evaluate(()=>window.botsFixture.a.holdCreate());await submit().click()
  await page.waitForFunction(()=>window.botsFixture.log.some(row=>row.method==="create"))
  const readsBeforeClose=(await log()).filter(row=>row.method==="list").length
  await dialog.getByRole("button",{name:w.cancel,exact:true}).click()
  await page.evaluate(()=>window.botsFixture.a.releaseCreate());await page.waitForTimeout(30)
  assert.equal(await dialog.count(),0)
  assert.equal((await log()).filter(row=>row.method==="list").length,readsBeforeClose,"late create cannot publish onCreated after close")
  await shot("catalog-late-create-after-close")
  page.catalogRequests=[...allRequests,...await log()]
  return ["101 registered Projects via manual pagination","same-label distinct IDs","metadata agents with unavailable native capabilities","no automatic drain or model invocation","first Send typed unavailable","initial/manual cursor/budget retry preserves form","revision/duplicate/foreign/repeated cursor refusal","late page after source change","late page after close","read-only metadata and fresh create gate","late revocation","effect fence after delayed status before host render","unknown create retains idempotency"]
}
/** The real Chat is mounted against adversarial, bounded synthetic SourceClients. */
async function boundedChecks(page,origin,language,width,shot,log) {
  const zh=language!=="en",w=zh?{all:"所有 Bots",message:"訊息",back:"返回 Bots",sessions:"收起 Bots",earlier:"較早的訊息",refresh:"檢查新訊息",more:"載入更多",details:"查看詳細內容",allow:"允許這次",trace:"查看處理過程",unknown:"結果未知；不會自動重送",quote:"引用回覆",clear:"取消引用",settings:"Bot 設定"}:{all:"All Bots",message:"Message",back:"Back to Bots",sessions:"Collapse Bots",earlier:"Earlier messages",refresh:"Check for updates",more:"Load more",details:"View details",allow:"Allow once",trace:"View processing steps",unknown:"Outcome unknown; no automatic retry",quote:"Reply",clear:"Remove reply",settings:"Bot settings"}
  const chat=page.locator(".pa-chat"),messageRows=chat.locator("[data-message-id]")
  const exactText=value=>new RegExp("^"+value.replace(/[.*+?^${}()|[\]\\]/g,"\\$&")+"$")
  // A Bot reply renders as Markdown in .pa-markdown; the person's own text stays a paragraph.
  const messageText=value=>chat.locator(".pa-message-bubble > p, .pa-message-bubble > .pa-markdown").filter({hasText:exactText(value)})
  const workResult=value=>chat.locator(".pa-chat-content > article.pa-work-task code").filter({hasText:exactText(value)})
  const editor=()=>chat.getByRole("textbox",{name:w.message,exact:true}),row=(name="Project assistant",machine="studio")=>page.locator(`.pa-bot-row[data-bot-key^='["${machine}",']`).filter({hasText:name})
  const info=page.locator(".pa-info-panel")
  const submit=()=>chat.locator(".pa-composer .pa-send").click()
  const openInfo=()=>chat.locator(".pa-chat-identity").click()
  const closeInfo=()=>info.getByRole("button",{name:zh?"關閉資訊":"Close information",exact:true}).click()
  const refresh=async()=>{
    const retry=chat.getByRole("alert").getByRole("button",{name:w.refresh,exact:true})
    if(await retry.isVisible()){await retry.click();return}
    await openInfo()
    await info.getByRole("button",{name:w.refresh,exact:true}).click()
    await closeInfo()
  }
  const alert=code=>chat.getByRole("alert").filter({hasText:code}).waitFor()
  const all=[];page.boundedRequests=all
  const fresh=async(setup,args)=>{
    if(page.url().startsWith(origin))all.push(...await log())
    await page.goto(origin+"/?lang="+language)
    if(setup)await page.evaluate(setup,args)
    await page.getByRole("button",{name:new RegExp(w.all)}).click();await row().waitFor()
  }
  const open=async()=>{await row().click();await editor().waitFor()}
  const ready=async()=>page.waitForFunction(()=>document.querySelector(".pa-editor textarea")?.disabled===false)
  const views=async mode=>(await log()).filter(r=>r.method==="view"&&r.request.mode===mode)
  const effectCount=async()=>(await log()).filter(r=>["send","answer","stop","edit","forget","saveMemory"].includes(r.method)).length
  const waitView=async(mode,n)=>page.waitForFunction(({mode,n})=>window.botsFixture.log.filter(r=>r.method==="view"&&r.request.mode===mode).length>n,{mode,n})
  const noLegacy=async()=>assert.deepEqual((await log()).filter(r=>["get","history","events","export"].includes(r.method)),[],"Chat never falls back to an aggregate read or events zero")

  const controlledRaces=async selected=>{
    if(!selected||selected==="permission")for(const mode of ["cursor","generation"]){
      await fresh(()=>{window.botsFixture.a.seedCollections(1);window.botsFixture.a.configureViews({forbidLegacy:true})});await open();await ready()
      const offer=chat.locator('[data-permission-id="offer-0"]')
      await offer.getByRole("button",{name:w.allow,exact:true}).waitFor()
      await page.evaluate(mode=>window.botsFixture.a.configureViews(mode==="cursor"?{detailFailure:"pa_cursor_expired"}:{detailFenceChange:true}),mode)
      await offer.getByRole("button",{name:w.allow,exact:true}).click()
      await alert(mode==="cursor"?"pa_cursor_expired":"pa_stale_generation")
      assert.equal(await editor().isDisabled(),true,"a failed second permission detail preflight must latch recovery")
      assert.equal((await log()).filter(row=>row.method==="answer").length,0)
      await page.evaluate(()=>window.botsFixture.a.configureViews({detailFailure:null,detailFenceChange:false}))
      await refresh();await ready();assert.equal((await log()).filter(row=>row.method==="answer").length,0)
      await shot("second-permission-preflight-"+mode)
    }
    if(!selected||selected==="parallel")for(const finish of ["same-fence-recovery","unmount"]){
      await fresh(()=>window.botsFixture.a.configureViews({forbidLegacy:true}));await open();await ready()
      await editor().fill("parallel preflight retains this draft")
      await page.evaluate(()=>window.botsFixture.a.holdSnapshots())
      const initial=(await views("snapshot")).length
      await submit();await waitView("snapshot",initial)
      await openInfo()
      await info.getByRole("tab",{name:w.settings,exact:true}).click()
      await info.locator(".pa-settings-part:not([hidden]) .pa-settings-disclosure > summary").first().click()
      await info.getByRole("textbox",{name:zh?"名稱":"Name",exact:true}).fill("Pending synthetic edit")
      await info.getByRole("button",{name:zh?"儲存":"Save",exact:true}).click();await waitView("snapshot",initial+1)
      await info.getByRole("button",{name:zh?"取消":"Cancel",exact:true}).click();await info.waitFor({state:"hidden"})
      if(finish==="same-fence-recovery"){
        await page.evaluate(()=>window.botsFixture.a.expireSameFence());await refresh();await waitView("snapshot",initial+2)
        // Release the recovery snapshot first. Both old preflights remain held until unlock.
        await page.evaluate(()=>window.botsFixture.a.releaseSnapshot(2));await ready()
        const aborted=await page.evaluate(()=>window.botsFixture.a.heldSnapshotSignals().slice(0,2))
        await page.evaluate(()=>{window.botsFixture.a.releaseSnapshot(0);window.botsFixture.a.releaseSnapshot(1)})
        await page.waitForTimeout(30)
        assert.equal((await log()).filter(row=>row.method==="send"||row.method==="edit").length,0,"an ignored-abort old preflight cannot enter an effect after same-fence recovery unlock")
        assert.deepEqual(aborted,[true,true],"recovery aborts every outstanding mutation, including the earlier Send")
        assert.equal(await editor().inputValue(),"parallel preflight retains this draft")
      }else{
        if(width<701)await chat.getByRole("button",{name:w.back,exact:true}).click()
        await row("Code review").click();await waitView("snapshot",initial+2)
        assert.deepEqual(await page.evaluate(()=>window.botsFixture.a.heldSnapshotSignals().slice(0,2)),[true,true],"unmount aborts all outstanding mutation preflights")
        await page.evaluate(()=>{for(const i of [0,1,2])window.botsFixture.a.releaseSnapshot(i)});await ready();await page.waitForTimeout(30)
        assert.equal((await log()).filter(row=>row.method==="send"||row.method==="edit").length,0)
      }
      await noLegacy();await shot("parallel-preflights-"+finish)
    }
  }
  const workOrder=async()=>{
    const order=()=>chat.locator('.pa-chat-content > article').evaluateAll(rows=>rows.map(row=>row.dataset.messageId??row.querySelector('small')?.textContent))
    await fresh();await open();await ready()
    const card=chat.locator('.pa-chat-content > article.pa-work-task').filter({hasText:'PA-01'})
    await card.waitFor()
    await editor().fill('New instruction after the old work');await submit()
    const latest=messageText('New instruction after the old work');await latest.waitFor()
    const latestRow=latest.locator('xpath=ancestor::article');const latestID=await latestRow.getAttribute('data-message-id')
    const assertOrder=async()=>{
      const ids=await order()
      assert.ok(ids.indexOf('PA-01')<ids.indexOf(latestID),'an old work card stays before the new instruction')
      assert.ok(ids.indexOf('bot-0-user')<ids.indexOf('PA-01'),'the work stays after its admission message')
    }
    await shot('work-order-after-send');await assertOrder()
    await page.evaluate(async()=>{
      const a=window.botsFixture.a,bot=a.bots[0],saved=await a.export(bot.key,new AbortController().signal),work=saved.work_links[0]
      a.push({id:crypto.randomUUID(),seq:0,at:'2026-11-13T00:00:00Z',version:'project-assistants.v1',kind:'work_updated',fence:work.fence,work:{...work,state:'landed',landing_commit:'updated-old-work'}})
    })
    await refresh();await workResult('updated-old-work').waitFor({state:"attached"});await assertOrder()
    await latestRow.locator('.pa-message-more').click();await latestRow.getByRole('menuitem',{name:w.quote,exact:true}).click()
    await chat.getByRole('button',{name:w.clear,exact:true}).waitFor()
    await shot('work-order-after-update')

    await fresh(()=>{window.botsFixture.a.seedHistory(150);window.botsFixture.a.seedWork('accepted');window.botsFixture.a.configureViews({forbidLegacy:true})});await open();await ready()
    await page.evaluate(()=>window.botsFixture.a.holdHistory())
    await chat.getByRole('button',{name:w.earlier,exact:true}).click();await waitView('history',0)
    await page.evaluate(()=>window.botsFixture.a.seedWork('landed'));await refresh();await workResult('newer-work-observation').waitFor({state:"attached"})
    await page.evaluate(()=>window.botsFixture.a.releaseHistory());await messageText('History message 1').waitFor()
    const ids=await order()
    assert.ok(ids.indexOf('history-1')<ids.indexOf('item')&&ids.indexOf('item')<ids.indexOf('history-2'),'a late history page restores the work admission position, retaining its new state')
    assert.equal(await workResult('newer-work-observation').count(),1)
    await noLegacy();await shot('work-order-after-history')
  }
  const sessionTask=async()=>{
    const example=zh?{
      request:"幫我用手機尺寸檢查 Bot 設定頁，看看模型選單和儲存按鈕。",
      reply:"我已請 Session 檢查模型選單與儲存流程。",
      raw:"390 px 手機版驗證\n\n• 模型可以切換，重新開啟後保留選擇。\n• 儲存後重新載入，偏好設定保持一致。\n• 320 px 時返回鍵與標題靠得太近。",
      followUp:"手機版的模型選單和儲存都正常。我會另外追蹤窄畫面上返回鍵和標題的間距。",
      task:"Session 任務",executing:"執行中",completed:"已完成",report:"Session 回報",
    }:{
      request:"Check the Bot settings on a phone, especially the model picker and Save button.",
      reply:"I asked a Session to check the model picker and save flow.",
      raw:"390 px mobile check\n\n• Model selection persists after reopening.\n• Saved preferences survive reload.\n• At 320 px the back button sits too close to the title.",
      followUp:"The model picker and Save work on mobile. I’ll follow up on the back button spacing in narrow layouts.",
      task:"Session task",executing:"Executing",completed:"Completed",report:"Session report",
    }
    await fresh(({request,reply})=>{
      window.botsFixture.a.setTaskExampleMessages(request,reply)
      window.botsFixture.a.setTaskExampleState("executing")
      document.getElementById("voice-owner").hidden=true
    },example)
    await open();await ready()
    const task=chat.locator("article").filter({has:page.locator("summary").getByText(example.task,{exact:true})})
    const header=task.locator("summary")
    await header.getByText(example.executing,{exact:true}).waitFor({timeout:5000})
    assert.ok((await task.boundingBox()).width<=280)
    assert.ok((await task.boundingBox()).height<=52)
    const reference=task.getByText("synthetic-session-task-reference-123456",{exact:true})
    assert.equal(await reference.isVisible(),false)
    await shot("session-task-running",chat)
    await header.press("Enter")
    await reference.waitFor()
    const anchored=await task.evaluate(el=>{
      window.exampleTask=el
      const before=document.querySelector('[data-message-id="bot-0-user"]')
      const after=document.querySelector('[data-message-id="bot-0-reply"]')
      return Boolean(before?.compareDocumentPosition(el)&Node.DOCUMENT_POSITION_FOLLOWING)
        && Boolean(el.compareDocumentPosition(after)&Node.DOCUMENT_POSITION_FOLLOWING)
    })
    assert.equal(anchored,true)
    await page.evaluate(()=>window.botsFixture.a.setTaskExampleState("completed"))
    await header.getByText(example.completed,{exact:true}).waitFor()
    assert.equal(await task.evaluate(el=>el===window.exampleTask),true,"completion updates the row at its admission position")
    assert.equal(await reference.isVisible(),true)
    await header.click()
    await page.evaluate(({raw,followUp})=>{
      const a=window.botsFixture.a
      a.emitMessage(raw,"work")
      a.emitMessage(followUp)
    },example)
    await messageText(example.followUp).waitFor()
    const report=chat.locator("summary").filter({hasText:exactText(example.report)})
    await report.click()
    await chat.getByText(example.raw,{exact:true}).waitFor()
    await shot("session-report-with-task",chat)
    await noLegacy()
  }
  if(process.env.BOTS_READ_RACE==="task"){
    await sessionTask();all.push(...await log());return ["compact task state, disclosed identity, in-place completion and original Session report"]
  }
  const sessionReports=async()=>{
    const label=zh?"Session 回報":"Session report"
    const raw="Browser check complete.\n\n- Settings saved\n- Mobile follow-up: "+"a".repeat(160)+"\n<script>ignored</script>"
    await fresh(({raw,label})=>{
      window.botsFixture.a.emitMessage(label)
      window.botsFixture.a.emitMessage(raw,"work")
    },{raw,label})
    await open();await ready()
    const report=chat.locator("details").filter({hasText:raw})
    const header=report.locator("summary").filter({hasText:exactText(label)})
    await header.waitFor({timeout:5000})
    await messageText(label).waitFor()
    const body=report.locator("p")
    assert.equal(await body.isVisible(),false)
    if(width<701)await header.click()
    else {await header.focus();await page.keyboard.press("Enter")}
    await body.waitFor()
    assert.equal(await body.textContent(),raw)
    assert.equal(await body.locator("script").count(),0)
    assert.equal(await body.evaluate(el=>el.scrollWidth<=el.clientWidth),true)
    await shot("session-report-open")
    await header.click()
    assert.equal(await body.isVisible(),false)
    const live="Second Session report received"
    await page.evaluate(text=>window.botsFixture.a.emitMessage(text,"work"),live)
    await chat.locator("details").filter({hasText:live}).locator("summary").waitFor()
    await noLegacy()
  }
  if(process.env.BOTS_READ_RACE==="report"){
    await sessionReports();all.push(...await log());return ["saved and live Session reports, source label, disclosure and literal content"]
  }
  if(process.env.BOTS_BACKGROUND_THREADS_ONLY){
    await fresh(()=>{window.botsFixture.a.bots[0].background_threads=3})
    await open();await ready();await openInfo()
    await info.getByRole("tab",{name:w.settings,exact:true}).click()
    const setting=info.getByRole("combobox",{name:zh?"背景工作並行數":"Background threads at once",exact:true})
    assert.equal(await setting.inputValue(),"3")
    await setting.selectOption("6")
    await info.getByRole("button",{name:zh?"儲存":"Save",exact:true}).click()
    await page.waitForFunction(()=>window.botsFixture.a.bots[0].background_threads===6)
    const edits=(await log()).filter(entry=>entry.method==="edit")
    assert.equal(edits.length,1)
    assert.equal(edits[0].request.settings.background_threads,6)
    await shot("background-concurrency")
    all.push(...await log())
    return ["saved background concurrency from the editable Bot settings"]
  }
  if(process.env.BOTS_READ_RACE==='order'){
    await workOrder();all.push(...await log());return ['work admission order after Send, live completion and delayed history; latest message actions']
  }
  if(process.env.BOTS_READ_RACE){
    if(!["permission","parallel"].includes(process.env.BOTS_READ_RACE))throw new Error("Unknown BOTS_READ_RACE")
    await controlledRaces(process.env.BOTS_READ_RACE);all.push(...await log());return ["controlled "+process.env.BOTS_READ_RACE+" preflight races"]
  }

  await workOrder()
  await sessionReports()

  await fresh(()=>window.botsFixture.a.seedHistory(600));await open()
  await messageText("History message 600").waitFor()
  assert.equal(await messageRows.count(),100,"initial window stays bounded even with 600 genuine journal events")
  const initial=await views("snapshot");assert.equal(initial[0].request.after_seq,0);assert.equal(initial[0].request.cursor,"");assert.equal(initial[0].request.source_epoch,"")
  await noLegacy()
  await page.evaluate(()=>{window.botsFixture.a.emitMessage("after frontier 603");window.botsFixture.a.emitMessage("after frontier 604")})
  await refresh();await messageText("after frontier 604").waitFor()
  assert.equal((await views("events"))[0].request.after_seq,602,"machine frontier includes another Bot's event602")
  assert.equal(await messageRows.count(),102)
  await editor().fill("retained reading draft")
  const scroll=chat.locator(".pa-chat-scroll");await scroll.evaluate(el=>{el.scrollTop=0;el.dispatchEvent(new Event("scroll"))})
  const before=await scroll.evaluate(el=>({height:el.scrollHeight,top:el.scrollTop}))
  await chat.getByRole("button",{name:w.earlier,exact:true}).click();await messageText("History message 401").waitFor()
  await page.waitForTimeout(30)
  const after=await scroll.evaluate(el=>({height:el.scrollHeight,top:el.scrollTop}))
  assert.ok(Math.abs((after.top-before.top)-(after.height-before.height))<3,"earlier page preserves the reading anchor")
  assert.equal(await editor().inputValue(),"retained reading draft")
  assert.ok((await views("history"))[0].request.cursor,"opaque history cursor is sent verbatim")
  await shot("bounded-600-frontier-602")

  await fresh(()=>{window.botsFixture.a.seedHistory(150);window.botsFixture.a.seedWork("accepted");window.botsFixture.a.configureViews({forbidLegacy:true})});await open();await ready()
  await page.evaluate(()=>window.botsFixture.a.holdHistory())
  await chat.getByRole("button",{name:w.earlier,exact:true}).click();await waitView("history",0)
  await page.evaluate(()=>window.botsFixture.a.seedWork("landed"));await refresh()
  await workResult("newer-work-observation").waitFor({state:"attached"})
  await page.evaluate(()=>window.botsFixture.a.releaseHistory());await messageText("History message 1").waitFor()
  assert.equal(await workResult("newer-work-observation").count(),1,"late old-generation work page cannot overwrite the newer event")
  await noLegacy();await shot("live-history-work-race")

  await fresh(()=>{window.botsFixture.a.seedCollections(101);window.botsFixture.a.configureViews({forbidLegacy:true});window.botsFixture.a.holdView("detail")});await open();await ready()
  await page.waitForFunction(()=>document.querySelectorAll("[data-permission-id]").length===100&&document.querySelector("[data-trace-count='100']"))
  assert.equal(await chat.locator("[data-permission-id]").count(),100)
  assert.equal(await chat.locator("[data-trace-count='100']").count(),1)
  assert.equal(await page.getByRole("button",{name:w.allow,exact:true}).count(),0,"summaries provide no answer control")
  assert.equal((await views("permissions")).every(r=>!r.request.cursor),true)
  assert.equal((await views("traces")).every(r=>!r.request.cursor),true)
  await page.evaluate(()=>window.botsFixture.a.releaseView())
  await chat.locator(".pa-permissions").getByRole("button",{name:w.more,exact:true}).click()
  await page.waitForFunction(()=>document.querySelectorAll("[data-permission-id]").length===101)
  await chat.locator(".pa-trace > summary").click()
  await chat.locator(".pa-trace .pa-trace-more-btn").click()
  await chat.locator("[data-trace-count='101']").waitFor()
  const permission=chat.locator('[data-permission-id="offer-0"]')
  await permission.getByRole("button",{name:w.allow,exact:true}).waitFor()
  await permission.getByRole("button",{name:w.allow,exact:true}).click()
  await page.waitForFunction(()=>window.botsFixture.log.filter(r=>r.method==="answer").length===1)
  const answers=(await log()).filter(r=>r.method==="answer");assert.equal(answers[0].request.offer_id,"offer-0")
  assert.equal(answers[0].request.option_id,"option-0")
  const trace=chat.locator('[data-trace-id="trace-collection-turn-0-read"]')
  await trace.getByText("Complete trace 0",{exact:true}).waitFor()
  await noLegacy();await shot("manual-101-summary-detail")

  // A detail response may ignore its aborted signal while generation changes.
  await fresh(()=>{window.botsFixture.a.seedCollections(2);window.botsFixture.a.holdView("detail")});await open();await ready()
  await waitView("detail",0)
  await page.evaluate(()=>window.botsFixture.a.expireCursor());await refresh()
  await chat.locator('[data-permission-id="new-generation-offer"]').waitFor()
  await page.evaluate(()=>window.botsFixture.a.releaseView());await page.waitForTimeout(30)
  assert.equal(await chat.locator('[data-permission-id="offer-1"]').count(),0)
  assert.equal((await log()).filter(r=>r.method==="answer").length,0,"old detail cannot restore an action")

  await fresh(()=>{window.botsFixture.a.seedHistory(150);window.botsFixture.a.configureViews({forbidLegacy:true})});await open();await ready()
  await editor().fill("snapshot failure retains this draft")
  const visible=await messageRows.evaluateAll(rows=>rows.map(row=>row.dataset.messageId))
  for(const code of ["pa_capacity_full","pa_offline","pa_revoked"]){
    await page.evaluate(code=>{window.botsFixture.a.configureViews({failure:code});window.botsFixture.a.expireCursor()},code)
    await refresh();await alert(code)
    assert.equal(await editor().isDisabled(),true)
    assert.equal(await editor().inputValue(),"snapshot failure retains this draft")
    assert.deepEqual(await messageRows.evaluateAll(rows=>rows.map(row=>row.dataset.messageId)),visible)
    const effects=await effectCount()
    await chat.locator(".pa-composer").evaluate(form=>form.dispatchEvent(new Event("submit",{bubbles:true,cancelable:true})))
    assert.equal(await effectCount(),effects)
  }
  await shot("snapshot-failure-retains-history-draft")
  if(width<701)await chat.getByRole("button",{name:w.back,exact:true}).click()
  await row("Code review").click();await ready();assert.equal(await editor().inputValue(),"")
  if(width<701)await chat.getByRole("button",{name:w.back,exact:true}).click()
  const snapshots=(await views("snapshot")).length
  await row().click();await editor().waitFor();assert.equal(await editor().isDisabled(),true)
  await waitView("snapshot",snapshots);await alert("pa_revoked")
  assert.equal(await chat.getByRole("alert").filter({hasText:"pa_read_only"}).count(),0,"a recovery latch is not a read-only source")
  const remountSnapshots=(await views("snapshot")).length
  await page.evaluate(()=>window.botsFixture.remount());await row().click();await editor().waitFor()
  await waitView("snapshot",remountSnapshots);await alert("pa_revoked")
  assert.equal(await editor().isDisabled(),true);assert.equal(await editor().inputValue(),"snapshot failure retains this draft")
  await page.evaluate(()=>{window.botsFixture.a.configureViews({failure:null});window.botsFixture.a.expireCursor();window.botsFixture.a.holdView("snapshot")})
  const retrySnapshots=(await views("snapshot")).length
  await refresh();await waitView("snapshot",retrySnapshots)
  assert.equal(await editor().isDisabled(),true)
  const effects=await effectCount();await chat.locator(".pa-composer").evaluate(form=>form.dispatchEvent(new Event("submit",{bubbles:true,cancelable:true})));assert.equal(await effectCount(),effects)
  await page.evaluate(()=>window.botsFixture.a.releaseView());await ready()
  await noLegacy();await shot("manual-bounded-recovery")
  // A later entry can recover through a verified read without replaying any effect.
  await page.evaluate(()=>{window.botsFixture.a.configureViews({failure:"pa_offline"});window.botsFixture.a.expireCursor()})
  await refresh();await alert("pa_offline")
  const effectsBeforeReentry=await effectCount(),snapshotsBeforeReentry=(await views("snapshot")).length
  await page.evaluate(()=>{window.botsFixture.a.configureViews({failure:null});window.botsFixture.a.holdView("snapshot");window.botsFixture.remount()})
  await row().click();await waitView("snapshot",snapshotsBeforeReentry)
  assert.equal(await editor().isDisabled(),true,"reentry remains locked until its snapshot validates")
  await page.evaluate(()=>window.botsFixture.a.releaseView());await ready()
  assert.equal(await editor().inputValue(),"snapshot failure retains this draft")
  assert.equal(await effectCount(),effectsBeforeReentry,"recovery only reads; no message or permission is replayed")
  assert.equal(await chat.getByRole("alert").count(),0)
  await shot("reentry-recovers-without-read-only")
  await page.evaluate(()=>{window.botsFixture.a.configureViews({failure:"pa_offline"});window.botsFixture.a.expireCursor()})
  await refresh();await alert("pa_offline")
  const resumeSnapshots=(await views("snapshot")).length,resumeEffects=await effectCount()
  await page.evaluate(()=>{window.botsFixture.a.configureViews({failure:null});window.botsFixture.a.holdView("snapshot");window.dispatchEvent(new PageTransitionEvent("pageshow",{persisted:true}))})
  await waitView("snapshot",resumeSnapshots);assert.equal(await editor().isDisabled(),true)
  await page.evaluate(()=>window.botsFixture.a.releaseView());await ready()
  assert.equal(await editor().inputValue(),"snapshot failure retains this draft")
  assert.equal(await effectCount(),resumeEffects,"returning from page cache only reads; it never replays an effect")
  await shot("page-return-recovers-without-read-only")
  // The new account has neither the failed latch nor the prior account's draft.
  await page.evaluate(()=>window.botsFixture.account("another-account"));await row().click();await ready();assert.equal(await editor().inputValue(),"")

  await fresh(()=>{window.botsFixture.a.seedPending(16,17);window.botsFixture.a.configureViews({forbidLegacy:true})});await open()
  // Queued messages behind a turn that is not running are already part of the conversation
  // (partitionMessages, as in Codync); the tray holds only those behind a running turn.
  await page.waitForFunction(()=>Array.from({length:16},(_,i)=>"Pending message "+i).every(text=>[...document.querySelectorAll(".pa-chat .pa-message.pa-mine .pa-message-bubble > p")].some(p=>p.textContent===text)))
  // Only this tab's own uncertain send holds the composer; unknown turns from
  // elsewhere stay visible and a new message is a new turn, as in Codync.
  assert.equal(await editor().isDisabled(),false)
  assert.equal(await chat.locator(".pa-queued").count(),0)
  assert.equal(await chat.getByText("not copied into summary",{exact:true}).count(),0)
  const paused=zh?"前一輪結果還無法確認，已收到的 16 則訊息暫停處理。請先核對上一輪已做的事。":"The previous result could not be confirmed. Waiting messages are paused (16). Check what the Bot already did."
  const queueStatus=chat.locator(".pa-chat-footer").getByRole("status").filter({hasText:paused})
  await queueStatus.getByRole("button",{name:w.refresh,exact:true}).waitFor()
  await noLegacy();await shot("queued-16-unknown-17")

  // A preflight snapshot that ignores abort must never enter the mutation effect.
  await fresh(()=>window.botsFixture.a.configureViews({forbidLegacy:true}));await open();await ready();await editor().fill("preflight must not send")
  await page.evaluate(()=>window.botsFixture.a.holdView("snapshot"))
  const preflights=(await views("snapshot")).length
  await submit();await waitView("snapshot",preflights)
  await page.evaluate(()=>window.botsFixture.a.expireCursor());await refresh();await waitView("snapshot",preflights+1)
  await page.evaluate(()=>window.botsFixture.a.releaseView());await ready()
  assert.equal((await log()).filter(r=>r.method==="send").length,0)
  assert.equal(await editor().inputValue(),"preflight must not send")
  await shot("ignored-abort-preflight")

  for(const operation of ["snapshot","detail"]){
    await fresh(()=>{window.botsFixture.a.seedCollections(1);window.botsFixture.a.configureViews({forbidLegacy:true})})
    await page.evaluate(operation=>window.botsFixture.a.holdView(operation),operation)
    await open()
    if(operation==="detail"){
      await ready();await chat.locator('[data-permission-id="offer-0"]').waitFor();await waitView("detail",0)
    }else await waitView("snapshot",0)
    await page.evaluate(()=>{window.botsFixture.revokeBeforeRender();window.botsFixture.a.releaseView()});await page.waitForTimeout(30)
    assert.equal(await page.getByRole("button",{name:w.allow,exact:true}).count(),0)
    assert.equal(await effectCount(),0)
    await shot("ignored-abort-revoked-"+operation)
  }

  await fresh(()=>window.botsFixture.a.configureViews({unsupported:true,forbidLegacy:true}));await open();await alert("pa_unsupported")
  assert.equal(await editor().isDisabled(),true);await noLegacy()
  assert.ok((await log()).some(r=>r.method==="list"),"API58 metadata remains readable while the new view is unsupported")
  await shot("legacy-metadata-view-unsupported")

  // Preserve the original keyboard, quote, unknown intent, source, focus and voice boundaries.
  await fresh();await open();await ready()
  // The person's own message offers Reply once tapped.
  await chat.locator('[data-message-id="bot-0-user"]').click()
  await chat.locator('[data-message-id="bot-0-user"]').getByRole("menuitem",{name:w.quote,exact:true}).click()
  await chat.getByRole("button",{name:w.clear,exact:true}).waitFor()
  await editor().fill("first Bot draft")
  if(width<701)await chat.getByRole("button",{name:w.back,exact:true}).click()
  await row("Code review").click();await ready();await editor().fill("second Bot draft")
  if(width<701)await chat.getByRole("button",{name:w.back,exact:true}).click()
  await row().click();await ready();assert.equal(await editor().inputValue(),"first Bot draft")
  await editor().evaluate(el=>el.dispatchEvent(new CompositionEvent("compositionstart",{bubbles:true,data:"中"})));await editor().press("Enter")
  assert.equal((await log()).filter(r=>r.method==="send").length,0)
  await editor().evaluate(el=>el.dispatchEvent(new CompositionEvent("compositionend",{bubbles:true,data:"中"})));await editor().press("Shift+Enter");assert.match(await editor().inputValue(),/\n/)
  await editor().fill("sent once");await editor().press("Enter")
  assert.equal(await editor().inputValue(),"sent once\n")
  assert.equal((await log()).filter(r=>r.method==="send").length,0)
  await editor().fill("sent once");await submit();await messageText("sent once").waitFor()
  assert.equal((await log()).filter(r=>r.method==="send").length,1)
  await editor().fill("unknown");await submit();await page.waitForFunction(()=>document.querySelector(".pa-editor textarea")?.disabled===true)
  await page.evaluate(()=>window.botsFixture.remount());await row().click();await editor().waitFor()
  assert.equal(await editor().isDisabled(),true);assert.equal(await editor().inputValue(),"unknown")
  await refresh();assert.equal(await editor().isDisabled(),true,"successful receipt-free snapshot cannot resolve an unknown intent")
  assert.equal((await log()).filter(r=>r.method==="send").length,2,"unknown effect is never replayed")
  await shot("unknown-survives-remount-and-snapshot")
  assert.equal(await page.getByRole("button",{name:"Existing voice stop control"}).isVisible(),true)
  assert.equal((await log()).filter(r=>r.method==="stop").length,0)
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
  if(width<701)await chat.getByRole("button",{name:w.back,exact:true}).click()
  await page.getByRole("button",{name:w.sessions,exact:true}).click()
  await page.getByRole("heading",{name:"Sessions",exact:true}).waitFor()
  await page.waitForFunction(()=>document.activeElement?.classList.contains("pa-dock"))
  await controlledRaces()
  all.push(...await log());return ["work admission order after Send, live completion and delayed history","600-event bounded initial100 / frontier602 / events603-604","opaque history and scroll anchor","old-generation work/event versus delayed older page","101 permission and trace summaries via manual more","detail-only answers / typed oversized detail","new generation cancels ignored-abort details","snapshot errors retain window and draft","same-account/Bot recovery latch across return and host remount","validated entry or manual snapshot is the only recovery unlock","new account isolation","queued16 and unknown17","ignored-abort mutation preflight and source revocation","API58 metadata / typed unsupported view / no fallback","IME, quote, draft switch, one send, unknown intent after remount","focus and voice ownership / no horizontal overflow","second permission preflight cursor/generation recovery latch","all parallel mutation controls abort on recovery/unmount and cannot resume after same-fence unlock"]
}
async function workProgressChecks(page,origin,language,shot,log) {
  const w=language==="en"
    ?{send:"Send message",stop:"Stop active and queued work",sending:"Sending the stop request",unknown:"The machine has not confirmed the stop request",accepted:"Stop request received. Waiting for the turn to end"}
    :{send:"傳送訊息",stop:"停止目前與待送工作",sending:"正在送出停止指令",unknown:"機器尚未確認停止結果",accepted:"機器已收到停止指令，等待回合結束"}
  const open=async()=>{
    await page.goto(origin+"/?lang="+language)
    await page.evaluate(()=>window.botsFixture.open())
    await page.locator(`.pa-bot-row[data-bot-key^='["studio",']`).filter({hasText:"Project assistant"}).click()
  }
  const chat=page.locator(".pa-chat")
  const measure=()=>chat.locator(".pa-composer").evaluate(form=>({
    height:form.getBoundingClientRect().height,
    button:form.querySelector(".pa-send").getBoundingClientRect().height,
  }))
  const pill=chat.locator(".pa-working-steps")
  const step=pill.locator("[data-trace-id]").first()
  const stop=chat.getByRole("button",{name:w.stop,exact:true})
  const commands=async()=>(await log()).filter(row=>row.method==="stop")
  for(const state of ["accepted","unknown"]) {
    await open()
    await chat.getByRole("button",{name:w.send,exact:true}).waitFor()
    const idle=await measure()
    await page.evaluate(()=>{
      window.botsFixture.a.seedWorkDetails()
      window.botsFixture.a.workTurn("running")
      window.botsFixture.a.holdView("detail")
      window.botsFixture.a.holdStop()
    })
    await stop.waitFor()
    await pill.locator("summary").waitFor()
    assert.deepEqual(await measure(),idle,"Stop must not enlarge the composer")
    assert.equal(await step.isVisible(),false,"current work is collapsed initially")
    assert.equal(await chat.locator("[data-trace-turn='turn-1']").count(),0,"live transcript has one place for this turn's details")
    await pill.locator("summary").click()
    const loading=pill.getByRole("status").filter({hasText:language==="en"?"Loading processing steps…":"正在載入處理步驟…"})
    await loading.waitFor()
    assert.equal(await pill.locator("[data-trace-id]").count(),0,"the loading indicator replaces placeholder rows")
    if(state==="unknown"){
      await page.evaluate(()=>window.botsFixture.a.configureViews({oversizedDetail:true}))
    }
    await page.evaluate(()=>window.botsFixture.a.releaseView())
    await loading.waitFor({state:"hidden"})
    if(state==="unknown"){
      const retry=pill.getByRole("button",{name:language==="en"?"Retry loading processing steps":"重試載入處理步驟"})
      await retry.waitFor()
      assert.equal(await pill.locator("pre").count(),0,"refused details are never presented as output")
      await page.evaluate(()=>window.botsFixture.a.configureViews({oversizedDetail:false}))
      await retry.click()
      await retry.waitFor({state:"hidden"})
    }
    await pill.getByText("Check the saved work before changing anything.",{exact:true}).waitFor()
    const thought=pill.locator(".pa-trace-thought")
    await thought.locator("summary").click()
    assert.equal(await thought.getByText("Check the saved work before changing anything.").isVisible(),false)
    await thought.locator("summary").click()
    await thought.getByText("Check the saved work before changing anything.").waitFor()
    const tool=pill.locator(".pa-trace-tool").filter({hasText:"Inspect the project files"})
    await tool.getByText("Inspect the project files",{exact:true}).waitFor()
    assert.equal(await tool.locator("[role=img]").count(),1)
    await pill.locator(".pa-trace-tool [data-failed]").waitFor()
    assert.equal(await tool.locator("pre").isVisible(),false)
    await tool.locator("summary").click()
    assert.equal(await tool.locator("pre").innerText(),"Found the project definition.")
    await pill.getByText("Read the project definition",{exact:false}).waitFor()
    assert.equal(await pill.locator('[data-plan-state="completed"]').count(),1)
    assert.equal(await pill.locator('[data-plan-state="pending"]').count(),1)
    const order=await pill.locator("[data-trace-id]").evaluateAll(rows=>rows.map(row=>row.getAttribute("data-trace-id").replace("trace-turn-1-","")))
    assert.deepEqual(order,["agent_thought_chunk","tool_call","tool_call_update","plan"])
    assert.equal(await pill.getByText("(pass)",{exact:true}).count(),0)
    assert.equal(await pill.getByText("host-aa-teamAvailable",{exact:false}).count(),0)
    await stop.click()
    await pill.getByRole("status").filter({hasText:w.sending}).waitFor()
    await page.waitForFunction(()=>window.botsFixture.log.filter(row=>row.method==="stop").length===1)
    assert.equal(await stop.isDisabled(),true)
    assert.equal(await stop.getAttribute("aria-busy"),"true")
    assert.equal((await commands())[0].request.turn_id,"turn-1")
    assert.deepEqual(await measure(),idle)
    await page.evaluate(state=>window.botsFixture.a.releaseStop(state),state)
    await pill.getByRole("status").filter({hasText:w[state]}).waitFor()
    assert.equal(await stop.isDisabled(),true)
    await shot("work-progress-stop-"+state)
    await page.evaluate(()=>window.botsFixture.a.workTurn("cancelled"))
    await chat.getByRole("button",{name:w.send,exact:true}).waitFor()
    await chat.locator("[data-trace-turn='turn-1'] summary").waitFor()
    assert.equal((await commands()).length,1)
  }
  return ["collapsed live steps and historical details","same composer height","one fenced Stop with pending, acknowledged, and unconfirmed results"]
}
async function attachmentRecoveryChecks(page,origin,language,shot,log) {
  const zh=language!=="en"
  await page.addInitScript(tag=>localStorage.setItem("ui_language",tag),language)
  await page.goto(origin+"/?header=1&lang="+language)
  await page.waitForFunction(()=>!!window.botsFixture)
  await page.evaluate(()=>{window.botsFixture.a.pictures=()=>true;window.botsFixture.open()})
  await page.locator(`.pa-bot-row[data-bot-key^='["studio",']`).filter({hasText:"Project assistant"}).click()
  const chat=page.locator(".pa-chat"),editor=chat.locator(".pa-editor textarea")
  await page.waitForFunction(()=>document.querySelector(".pa-editor textarea")?.disabled===false)
  await editor.fill("First line")
  await editor.press("Enter")
  assert.equal(await editor.inputValue(),"First line\n")
  assert.equal(await editor.getAttribute("enterkeyhint"),"enter")
  assert.equal((await log()).filter(r=>r.method==="send").length,0,"Enter sent the draft")
  const dropped=await chat.locator(process.env.BOTS_ATTACHMENT_REPRO==="recovery"?".pa-composer":".pa-chat-scroll").evaluate(async node=>{
    const data=new DataTransfer()
    const canvas=document.createElement("canvas")
    canvas.width=80;canvas.height=60
    const ctx=canvas.getContext("2d")
    ctx.fillStyle="#2490ca";ctx.fillRect(0,0,80,60)
    const bytes=await new Promise(resolve=>canvas.toBlob(resolve,"image/png"))
    data.items.add(new File([bytes],"fixture.png"))
    node.dispatchEvent(new DragEvent("dragover",{bubbles:true,cancelable:true,dataTransfer:data}))
    const drop=new DragEvent("drop",{bubbles:true,cancelable:true,dataTransfer:data})
    node.dispatchEvent(drop)
    return drop.defaultPrevented
  })
  assert.equal(dropped,true,"the Bot chat did not accept a dropped picture")
  await page.waitForFunction(()=>document.querySelector(".pa-pictures img")?.naturalWidth===80)
  assert.equal((await log()).filter(r=>r.method==="send").length,0,"dropping a picture sent a message")
  const preview=chat.getByRole("button",{name:zh?"圖片預覽":"Image preview",exact:true})
  const thumbnail=preview.locator("img")
  const original=await thumbnail.getAttribute("src")
  const markup=page.locator(".image-markup")
  const canvas=markup.locator("canvas[data-ready=true]")
  await preview.click()
  await canvas.waitFor({timeout:4000})
  await markup.getByRole("button",{name:zh?"放大":"Zoom in",exact:true}).click()
  assert.ok((await canvas.boundingBox()).width>80)
  const draw=async()=>{
    const {x,y,width,height}=await canvas.boundingBox()
    const points=[[.3,.5],[.5,.25],[.7,.5],[.5,.75],[.3,.5]].map(([u,v])=>({x:x+width*u,y:y+height*v}))
    if(page.touchInput){
      await page.touchInput.send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:[points[0]]})
      for(const point of points.slice(1))await page.touchInput.send("Input.dispatchTouchEvent",{type:"touchMove",touchPoints:[point]})
      await page.touchInput.send("Input.dispatchTouchEvent",{type:"touchEnd",touchPoints:[]})
    }else{
      await page.mouse.move(points[0].x,points[0].y)
      await page.mouse.down()
      for(const point of points.slice(1))await page.mouse.move(point.x,point.y)
      await page.mouse.up()
    }
    assert.equal(await markup.getByRole("button",{name:zh?/復原/:/Undo/}).isEnabled(),true)
  }
  await draw()
  await markup.getByRole("button",{name:zh?"取消":"Cancel",exact:true}).click()
  await markup.waitFor({state:"hidden"})
  assert.equal(await thumbnail.getAttribute("src"),original)
  await preview.click()
  await canvas.waitFor()
  await draw()
  await markup.getByRole("button",{name:zh?/復原/:/Undo/}).click()
  await markup.getByRole("button",{name:zh?"完成":"Done",exact:true}).click()
  assert.equal(await thumbnail.getAttribute("src"),original)
  await preview.click()
  await canvas.waitFor()
  await draw()
  await shot("picture-markup")
  await markup.getByRole("button",{name:zh?"完成":"Done",exact:true}).click()
  await markup.waitFor({state:"hidden"})
  const marked=await thumbnail.getAttribute("src")
  assert.notEqual(marked,original)
  assert.equal(await editor.inputValue(),"First line\n")
  assert.equal((await log()).filter(r=>r.method==="send").length,0,"saving a mark sent the draft")
  await editor.fill("unknown")
  await chat.locator(".pa-composer .pa-send").click()
  const pending=chat.locator(".pa-chat-scroll .pa-unsent").first()
  await pending.waitFor()
  const picture=(await log()).find(row=>row.method==="send").request.images[0]
  assert.equal(picture,marked)
  assert.match(picture,/^data:image\/png;base64,/)
  assert.equal(await page.evaluate(async url=>{
    const image=new Image()
    image.src=url
    await image.decode()
    const canvas=document.createElement("canvas")
    canvas.width=image.width;canvas.height=image.height
    const ctx=canvas.getContext("2d")
    ctx.drawImage(image,0,0)
    const rgba=ctx.getImageData(0,0,canvas.width,canvas.height).data
    return rgba.some((red,i)=>i%4===0&&red===255&&rgba[i+1]===45&&rgba[i+2]===45)
  },picture),true,"the sent image has no red ink")
  assert.equal(await editor.inputValue(),"")
  await editor.fill("A new draft")
  const retry=pending.getByRole("button",{name:zh?"重試送出":"Retry sending",exact:true})
  await shot("held-picture")
  await retry.click()
  await retry.waitFor()
  const writes=(await log()).filter(r=>r.method==="send")
  assert.equal(writes.length,2)
  assert.deepEqual(writes[1].request,writes[0].request,"retry must keep the original picture and command ID")
  assert.equal(await editor.inputValue(),"A new draft")
  return ["Enter inserts a newline without sending","zoom, circle, cancel, undo, and send marked pixels","drop attaches a picture without sending","uncertain send stays in the transcript","retry keeps the picture and command ID","a separate draft survives retry"]
}

// The composer's trailing button places the call while the box is empty; the live call's
// controls sit under the header. Browser speech is stubbed; no audio or model runs.
async function callPlacementChecks(page,origin,language,width,shot,log) {
  const zh=language!=="en",w=zh?{all:"所有 Bots",message:"訊息",call:"語音通話 · Project assistant",send:"傳送訊息",stop:"停止目前與待送工作",end:"掛斷",listening:"正在聆聽"}:{all:"All Bots",message:"Message",call:"Voice call · Project assistant",send:"Send message",stop:"Stop active and queued work",end:"Hang up",listening:"Listening"}
  await page.addInitScript(()=>{
    const voice={opened:0,closed:0,settings:null}
    window.fixtureVoice=voice
    const real=window.fetch.bind(window)
    // The copilot key check answers "no key" only when the test releases it.
    window.fetch=(input,init)=>String(input?.url??input).includes("/v1/settings/copilot")
      ?new Promise(resolve=>{voice.settings=()=>resolve(new Response(JSON.stringify({key_set:false}),{status:200,headers:{"Content-Type":"application/json"}}));if(sessionStorage.getItem("fixtureSettingsNow"))voice.settings()})
      :real(input,init)
    const synth=new EventTarget();synth.getVoices=()=>[{lang:"en-US",localService:true},{lang:"zh-TW",localService:true}]
    synth.speak=()=>{};synth.cancel=()=>{}
    class Recognition {static async available(){return "available"};start(){};stop(){};abort(){}}
    Object.defineProperty(Recognition.prototype,"processLocally",{value:false,writable:true})
    const track=Object.assign(new EventTarget(),{readyState:"live",enabled:false,stop:()=>voice.closed++})
    Object.defineProperty(window,"SpeechRecognition",{configurable:true,value:Recognition})
    Object.defineProperty(window,"speechSynthesis",{configurable:true,value:synth})
    Object.defineProperty(window,"SpeechSynthesisUtterance",{configurable:true,value:class {}})
    Object.defineProperty(navigator,"mediaDevices",{configurable:true,value:{getUserMedia:async()=>{voice.opened++;return {getAudioTracks:()=>[track],getTracks:()=>[track]}}}})
    // Every state the trailing button passes through, in order.
    window.trail=[]
    const note=()=>{
      const b=document.querySelector(".pa-composer .pa-send")
      const now=!b?"none":b.hasAttribute("data-call")?"call":b.hasAttribute("data-stop")?"stop":"send"
      if(window.trail.at(-1)!==now)window.trail.push(now)
    }
    new MutationObserver(note).observe(document,{subtree:true,childList:true,attributes:true})
  })
  await page.goto(origin+"/?lang="+language)
  await page.getByRole("button",{name:new RegExp(w.all)}).click()
  await page.locator(`.pa-bot-row[data-bot-key^='["studio",']`).filter({hasText:"Project assistant"}).click()
  const chat=page.locator(".pa-chat"),editor=chat.getByRole("textbox",{name:w.message,exact:true}),trailing=chat.locator(".pa-composer .pa-send")
  await editor.waitFor()
  await page.waitForFunction(()=>document.querySelector(".pa-editor textarea")?.disabled===false)
  assert.equal(await chat.locator(".pa-bot-call-start").count(),0,"no call row of its own")
  // While the key check reads, the trailing button stays send: nothing to hide later.
  await page.waitForTimeout(300)
  assert.equal(await trailing.getAttribute("aria-label"),w.send)
  assert.equal(await page.evaluate(()=>window.trail.includes("call")),false)
  await page.evaluate(()=>window.fixtureVoice.settings())
  const call=chat.getByRole("button",{name:w.call,exact:true})
  await call.waitFor()
  assert.equal(await call.evaluate(el=>el.closest(".pa-composer")!==null&&el.classList.contains("pa-send")),true,"the call is the composer's trailing button")
  const box=await call.boundingBox()
  assert.ok(box.width>40&&box.width<50&&Math.round(box.height)===34,"a wider capsule than send: "+JSON.stringify(box))
  await shot("call-empty")
  await editor.fill("Hello")
  assert.equal(await trailing.getAttribute("aria-label"),w.send)
  assert.equal(await call.count(),0)
  await shot("call-typed")
  // Sending: the call never blinks out to send and back while the box is empty.
  const before=await page.evaluate(()=>window.trail.length)
  await trailing.click()
  await page.waitForFunction(()=>document.querySelector(".pa-editor textarea")?.value==="")
  await page.waitForTimeout(1500)
  const during=await page.evaluate(n=>window.trail.slice(n),before)
  assert.equal(during.includes("none"),false,"the trailing button never vanishes: "+during)
  const firstCall=during.indexOf("call")
  assert.ok(firstCall<0||!during.slice(firstCall).includes("send"),"no call → send → call flicker after sending: "+during)
  assert.equal(await call.isVisible(),true,"the call stays in place while the send is in flight")
  assert.equal(await call.isDisabled(),true,"and cannot be placed until the send settles")
  await shot("call-sending")
  // A fresh chat with nothing in flight places the call.
  const reopen=async setup=>{
    await page.evaluate(()=>{localStorage.clear();sessionStorage.clear();sessionStorage.setItem("fixtureSettingsNow","1")})
    await page.goto(origin+"/?lang="+language)
    if(setup)await page.evaluate(setup)
    await page.getByRole("button",{name:new RegExp(w.all)}).click()
    await page.locator(`.pa-bot-row[data-bot-key^='["studio",']`).filter({hasText:"Project assistant"}).click()
    await editor.waitFor()
  }
  await reopen()
  await page.waitForFunction(label=>document.querySelector(`.pa-composer .pa-send[aria-label="${label}"]`)?.disabled===false,w.call)
  // A live call's controls sit under the header, not above the composer.
  await call.click()
  const bar=chat.locator("section.pa-bot-call")
  await bar.getByText(w.listening).waitFor()
  const [barBox,scrollBox,composerBox]=await Promise.all([bar.boundingBox(),chat.locator(".pa-chat-scroll").boundingBox(),chat.locator(".pa-composer").boundingBox()])
  assert.ok(barBox.y+barBox.height<=scrollBox.y+1,"call bar sits above the transcript")
  assert.ok(barBox.y+barBox.height<composerBox.y-100,"call bar is not stacked on the composer")
  assert.equal(await chat.getByRole("button",{name:w.call,exact:true}).count(),0,"no call button during a call")
  await shot("call-live")
  await bar.getByRole("button",{name:w.end,exact:true}).click()
  await bar.waitFor({state:"detached"})
  assert.deepEqual(await page.evaluate(()=>[window.fixtureVoice.opened,window.fixtureVoice.closed]),[1,1])
  await call.waitFor()
  // A running Bot turns the empty box's trailing button into stop.
  await reopen(()=>window.botsFixture.a.seedCollections(1))
  await chat.locator(".pa-composer .pa-send[data-stop]").waitFor()
  assert.equal(await trailing.getAttribute("aria-label"),w.stop)
  assert.equal(await call.count(),0)
  await shot("call-running-stop")
  return ["send stays while the key check reads","empty box offers the call capsule","typing turns it to send","sending never blinks the call out and back","the call waits disabled while a send is in flight","live call bar sits under the header","hang up releases the microphone","a running Bot shows stop"]
}
let forbidden=0
const server=createServer((req,res)=>{const url=new URL(req.url,"http://fixture");if(url.pathname.startsWith("/v1")){forbidden++;res.writeHead(500);res.end("No live API in fixture");return}const text=url.pathname==="/"?html:content.get(url.pathname);if(text===undefined){res.writeHead(404);res.end();return}res.setHeader("Content-Type",url.pathname.endsWith(".js")?"text/javascript":url.pathname.endsWith(".css")?"text/css":url.pathname.endsWith(".json")?"application/json":"text/html");res.end(text)})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,results=[]
const failures=[]
try {
  for(const engine of [chromium,webkit].filter(e=>!process.env.BOTS_ENGINE||e.name()===process.env.BOTS_ENGINE))for(const width of (process.env.BOTS_WIDTH?[Number(process.env.BOTS_WIDTH)]:[390,1440]))for(const language of (process.env.BOTS_LANGUAGE?[process.env.BOTS_LANGUAGE]:["zh-Hant","en"])) {
    const w=language==="en"?{allBots:"All Bots",select:"Select a Bot to start reading",more:"Load more",message:"Message",back:"Back to Bots",permission:"Permission request",always:"Always allow (unavailable)",allow:"Allow once",unknown:"Outcome unknown; no automatic retry",create:"Create Bot",name:"Name",submit:"Create",sessions:"Collapse Bots",earlier:"Earlier messages",refresh:"Check for updates"}:{allBots:"所有 Bots",select:"選擇一位 Bot 開始閱讀",more:"載入更多",message:"訊息",back:"返回 Bots",permission:"權限請求",always:"一律允許（不提供）",allow:"允許這次",unknown:"結果未知；不會自動重送",create:"新增 Bot",name:"名稱",submit:"建立",sessions:"收起 Bots",earlier:"較早的訊息",refresh:"檢查新訊息"}
    const browser=await engine.launch({headless:true,...(engine.name()==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{})}),name=`${engine.name()}-${width}-${language}`
    const page=await browser.newPage({viewport:{width,height:width===1440?960:width===1280?800:844},reducedMotion:"reduce",hasTouch:!!(process.env.BOTS_WORK_PROGRESS_ONLY||process.env.BOTS_ATTACHMENTS_ONLY)&&width<701})
    if(process.env.BOTS_ATTACHMENTS_ONLY&&engine.name()==="chromium"&&width<701)page.touchInput=await page.context().newCDPSession(page)
    const consoleErrors=[];page.on("pageerror",error=>consoleErrors.push(error.message))
    await page.route("**/*",route=>{const url=new URL(route.request().url());if(url.origin!==origin||url.pathname.startsWith("/v1")){forbidden++;return route.abort()}return route.continue()})
    const shot=async(code,scope)=>{
      const file=path.join(artifacts,`${code}-${name}.png`)
      return scope?scope.screenshot({path:file}):page.screenshot({path:file,fullPage:true})
    }
    const log=()=>page.evaluate(()=>window.botsFixture.log)
    // A row reads its name, its Project, then its machine when there are several.
    const row=(bot="Project assistant",machine="studio")=>page.getByRole("button",{name:new RegExp(`^${bot}, [^,]+, ${machine}(,|$)`)})
    try {
      if(process.env.BOTS_ATTACHMENTS_ONLY){
        const checks=await attachmentRecoveryChecks(page,origin,language,shot,log)
        assert.deepEqual(consoleErrors,[])
        results.push({engine:engine.name(),width,language,status:"pass",checks});continue
      }
      if(process.env.BOTS_CALL_ONLY){
        const checks=await callPlacementChecks(page,origin,language,width,shot,log)
        assert.deepEqual(consoleErrors,[])
        results.push({engine:engine.name(),width,language,status:"pass",checks});continue
      }
      if(process.env.BOTS_CATALOG_ONLY){
        const checks=await catalogChecks(page,origin,language,shot,log)
        assert.deepEqual(consoleErrors,[])
        await writeFile(path.join(artifacts,`interactions-${name}.json`),JSON.stringify({log:page.catalogRequests,consoleErrors,checks,modelInvocations:0},null,2))
        results.push({engine:engine.name(),width,language,status:"pass",checks});continue
      }
      const checks=process.env.BOTS_WORK_PROGRESS_ONLY?await workProgressChecks(page,origin,language,shot,log):await boundedChecks(page,origin,language,width,shot,log)
      assert.deepEqual(consoleErrors,[])
      await writeFile(path.join(artifacts,`interactions-${name}.json`),JSON.stringify({log:page.boundedRequests,consoleErrors,checks,modelInvocations:0},null,2))
      results.push({engine:engine.name(),width,language,status:"pass",checks})
    }catch(error){await shot("FAIL");await writeFile(path.join(artifacts,`interactions-${name}.json`),JSON.stringify({log:[...page.boundedRequests??[],...await log()],consoleErrors,error:error.stack},null,2));failures.push({name,error:error.stack});results.push({engine:engine.name(),width,status:"fail",error:error.message})}
    finally{await page.close();await browser.close()}
  }
  assert.ok(results.length>0,"no browser case was selected")
  assert.equal(forbidden,0,"fixture touched a /v1 API")
} finally {await new Promise(resolve=>server.close(resolve));await writeFile(path.join(artifacts,"browser-results.json"),JSON.stringify({results,failures,forbidden,origin,synthetic:true},null,2))}
console.log(JSON.stringify({results,forbidden},null,2))
if(failures.length)process.exitCode=1
