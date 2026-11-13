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
// This matrix verifies injected metadata behavior; real transport/store acceptance is separate.
async function catalogChecks(page,origin,language,shot,log) {
  const zh=language!=="en",w=zh?{create:"新增 Bot",submit:"建立",cancel:"取消",name:"名稱",machine:"機器",agent:"Agent",instructions:"固定指示",more:"載入更多",retry:"重試",search:"搜尋 Bot 或 Project",message:"訊息"}:{create:"Create Bot",submit:"Create",cancel:"Cancel",name:"Name",machine:"Machine",agent:"Agent",instructions:"Standing instructions",more:"Load more",retry:"Try again",search:"Search Bot or Project",message:"Message"}
  const dialog=page.getByRole("dialog"),project=()=>dialog.getByRole("combobox",{name:"Project",exact:true}),submit=()=>dialog.getByRole("button",{name:w.submit,exact:true})
  const allRequests=[]
  const fresh=async(options={})=>{
    if(page.url().startsWith(origin))allRequests.push(...await log())
    await page.goto(origin+"/?lang="+language)
    await page.evaluate(options=>{window.botsFixture.a.configureProjects(options);window.botsFixture.open()},options)
    await page.getByRole("button",{name:w.create,exact:true}).waitFor()
  }
  const open=async()=>{await page.getByRole("button",{name:w.create,exact:true}).click();await dialog.waitFor()}
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
  await page.getByRole("button",{name:/^Catalog Bot, studio,/}).click()
  await page.getByRole("textbox",{name:w.message,exact:true}).fill("First send cannot admit a provider")
  await page.getByRole("textbox",{name:w.message,exact:true}).press("Enter")
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
  const zh=language!=="en",w=zh?{all:"所有 Bots",message:"訊息",back:"返回 Bots",sessions:"返回 Sessions",earlier:"較早的訊息",refresh:"檢查新訊息",more:"載入更多",details:"查看詳細內容",allow:"允許這次",trace:"查看處理過程",unknown:"結果未知；不會自動重送",quote:"引用回覆",clear:"取消引用",settings:"Bot 設定"}:{all:"All Bots",message:"Message",back:"Back to Bots",sessions:"Back to Sessions",earlier:"Earlier messages",refresh:"Check for updates",more:"Load more",details:"View details",allow:"Allow once",trace:"View processing steps",unknown:"Outcome unknown; no automatic retry",quote:"Reply",clear:"Remove reply",settings:"Bot settings"}
  const chat=page.locator(".pa-chat"),messageRows=chat.locator("[data-message-id]")
  const exactText=value=>new RegExp("^"+value.replace(/[.*+?^${}()|[\]\\]/g,"\\$&")+"$")
  const messageText=value=>chat.locator(".pa-message > p").filter({hasText:exactText(value)})
  const workResult=value=>chat.locator(".pa-chat-content > article.pa-card code").filter({hasText:exactText(value)})
  const editor=()=>chat.getByRole("textbox",{name:w.message,exact:true}),row=(name="Project assistant",machine="studio")=>page.getByRole("button",{name:new RegExp(`^${name}, ${machine},`)})
  const refresh=()=>chat.locator(".pa-chat-tools").getByRole("button",{name:w.refresh,exact:true}).click()
  const alert=code=>chat.getByRole("alert").filter({hasText:code}).waitFor()
  const all=[];page.boundedRequests=all
  const fresh=async setup=>{
    if(page.url().startsWith(origin))all.push(...await log())
    await page.goto(origin+"/?lang="+language)
    if(setup)await page.evaluate(setup)
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
      await offer.getByRole("button",{name:w.details,exact:true}).click();await offer.getByRole("button",{name:w.allow,exact:true}).waitFor()
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
      await editor().press("Enter");await waitView("snapshot",initial)
      await chat.getByRole("button",{name:w.settings,exact:true}).click()
      const dialog=page.getByRole("dialog")
      await dialog.getByRole("button",{name:zh?"儲存":"Save",exact:true}).click();await waitView("snapshot",initial+1)
      await dialog.getByRole("button",{name:zh?"取消":"Cancel",exact:true}).click();await dialog.waitFor({state:"hidden"})
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
  if(process.env.BOTS_READ_RACE){
    if(!["permission","parallel"].includes(process.env.BOTS_READ_RACE))throw new Error("Unknown BOTS_READ_RACE")
    await controlledRaces(process.env.BOTS_READ_RACE);all.push(...await log());return ["controlled "+process.env.BOTS_READ_RACE+" preflight races"]
  }

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
  await workResult("newer-work-observation").waitFor()
  await page.evaluate(()=>window.botsFixture.a.releaseHistory());await messageText("History message 1").waitFor()
  assert.equal(await workResult("newer-work-observation").count(),1,"late old-generation work page cannot overwrite the newer event")
  await noLegacy();await shot("live-history-work-race")

  await fresh(()=>{window.botsFixture.a.seedCollections(101);window.botsFixture.a.configureViews({forbidLegacy:true})});await open();await ready()
  await page.waitForFunction(()=>document.querySelectorAll("[data-permission-id]").length===100&&document.querySelectorAll("[data-trace-id]").length===100)
  assert.equal(await chat.locator("[data-permission-id]").count(),100)
  assert.equal(await chat.locator("[data-trace-id]").count(),100)
  assert.equal(await page.getByRole("button",{name:w.allow,exact:true}).count(),0,"summaries provide no answer control")
  assert.equal((await views("permissions")).every(r=>!r.request.cursor),true)
  assert.equal((await views("traces")).every(r=>!r.request.cursor),true)
  await chat.locator(".pa-permissions").getByRole("button",{name:w.more,exact:true}).click()
  await page.waitForFunction(()=>document.querySelectorAll("[data-permission-id]").length===101)
  await chat.locator(".pa-trace > summary").click()
  await chat.locator(".pa-trace").getByRole("button",{name:w.more,exact:true}).click()
  await page.waitForFunction(()=>document.querySelectorAll("[data-trace-id]").length===101)
  const permission=chat.locator('[data-permission-id="offer-0"]')
  await permission.getByRole("button",{name:w.details,exact:true}).click()
  await permission.getByRole("button",{name:w.allow,exact:true}).waitFor()
  await permission.getByRole("button",{name:w.allow,exact:true}).click()
  await page.waitForFunction(()=>window.botsFixture.log.filter(r=>r.method==="answer").length===1)
  const answers=(await log()).filter(r=>r.method==="answer");assert.equal(answers[0].request.offer_id,"offer-0")
  assert.equal(answers[0].request.option_id,"option-0")
  const trace=chat.locator('[data-trace-id="trace-collection-turn-0-read"]')
  await page.evaluate(()=>window.botsFixture.a.configureViews({oversizedDetail:true}))
  await trace.getByRole("button",{name:w.details,exact:true}).click();await alert("pa_capacity_full")
  assert.equal(await trace.locator("pre").count(),0,"an oversized indivisible detail is never truncated into success")
  await page.evaluate(()=>window.botsFixture.a.configureViews({oversizedDetail:false}))
  await trace.getByRole("button",{name:w.details,exact:true}).click();await trace.getByText("Complete trace 0",{exact:true}).waitFor()
  await noLegacy();await shot("manual-101-summary-detail")

  // A detail response may ignore its aborted signal while generation changes.
  await page.evaluate(()=>window.botsFixture.a.holdView("detail"))
  const detailReads=(await views("detail")).length
  await chat.locator('[data-permission-id="offer-1"]').getByRole("button",{name:w.details,exact:true}).click();await waitView("detail",detailReads)
  await page.evaluate(()=>window.botsFixture.a.expireCursor());await refresh()
  await chat.locator('[data-permission-id="new-generation-offer"]').waitFor()
  await page.evaluate(()=>window.botsFixture.a.releaseView());await page.waitForTimeout(30)
  assert.equal(await chat.locator('[data-permission-id="offer-1"]').count(),0)
  assert.equal((await log()).filter(r=>r.method==="answer").length,1,"old detail cannot restore an action")

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
  assert.equal((await views("snapshot")).length,snapshots,"returning to a failed Bot requires manual recovery")
  await page.evaluate(()=>window.botsFixture.remount());await row().click();await editor().waitFor()
  assert.equal(await editor().isDisabled(),true);assert.equal(await editor().inputValue(),"snapshot failure retains this draft")
  await page.evaluate(()=>{window.botsFixture.a.configureViews({failure:null});window.botsFixture.a.expireCursor();window.botsFixture.a.holdView("snapshot")})
  const retrySnapshots=(await views("snapshot")).length
  await refresh();await waitView("snapshot",retrySnapshots)
  assert.equal(await editor().isDisabled(),true)
  const effects=await effectCount();await chat.locator(".pa-composer").evaluate(form=>form.dispatchEvent(new Event("submit",{bubbles:true,cancelable:true})));assert.equal(await effectCount(),effects)
  await page.evaluate(()=>window.botsFixture.a.releaseView());await ready()
  await noLegacy();await shot("manual-bounded-recovery")
  // The new account has neither the failed latch nor the prior account's draft.
  await page.evaluate(()=>window.botsFixture.account("another-account"));await row().click();await ready();assert.equal(await editor().inputValue(),"")

  await fresh(()=>{window.botsFixture.a.seedPending(16,17);window.botsFixture.a.configureViews({forbidLegacy:true})});await open()
  await page.waitForFunction(()=>document.querySelectorAll(".pa-queued").length===16)
  assert.equal(await editor().isDisabled(),true)
  assert.equal(await chat.locator(".pa-queued").count(),16)
  assert.equal(await chat.getByText("not copied into summary",{exact:true}).count(),0)
  await chat.locator(".pa-chat-footer").getByText(w.unknown+" · 17",{exact:true}).waitFor();await noLegacy();await shot("queued-16-unknown-17")

  // A preflight snapshot that ignores abort must never enter the mutation effect.
  await fresh(()=>window.botsFixture.a.configureViews({forbidLegacy:true}));await open();await ready();await editor().fill("preflight must not send")
  await page.evaluate(()=>window.botsFixture.a.holdView("snapshot"))
  const preflights=(await views("snapshot")).length
  await editor().press("Enter");await waitView("snapshot",preflights)
  await page.evaluate(()=>window.botsFixture.a.expireCursor());await refresh();await waitView("snapshot",preflights+1)
  await page.evaluate(()=>window.botsFixture.a.releaseView());await ready()
  assert.equal((await log()).filter(r=>r.method==="send").length,0)
  assert.equal(await editor().inputValue(),"preflight must not send")
  await shot("ignored-abort-preflight")

  for(const operation of ["snapshot","detail"]){
    await fresh(()=>{window.botsFixture.a.seedCollections(1);window.botsFixture.a.configureViews({forbidLegacy:true})})
    if(operation==="snapshot")await page.evaluate(()=>window.botsFixture.a.holdView("snapshot"))
    await open()
    if(operation==="detail"){
      await ready();await chat.locator('[data-permission-id="offer-0"]').waitFor();await page.evaluate(()=>window.botsFixture.a.holdView("detail"))
      await chat.locator('[data-permission-id="offer-0"]').getByRole("button",{name:w.details,exact:true}).click();await waitView("detail",0)
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
  await chat.locator('[data-message-id="bot-0-user"]').getByRole("button",{name:w.quote,exact:true}).click()
  await chat.getByRole("button",{name:w.clear,exact:true}).waitFor()
  await editor().fill("first Bot draft")
  if(width<701)await chat.getByRole("button",{name:w.back,exact:true}).click()
  await row("Code review").click();await ready();await editor().fill("second Bot draft")
  if(width<701)await chat.getByRole("button",{name:w.back,exact:true}).click()
  await row().click();await ready();assert.equal(await editor().inputValue(),"first Bot draft")
  await editor().evaluate(el=>el.dispatchEvent(new CompositionEvent("compositionstart",{bubbles:true,data:"中"})));await editor().press("Enter")
  assert.equal((await log()).filter(r=>r.method==="send").length,0)
  await editor().evaluate(el=>el.dispatchEvent(new CompositionEvent("compositionend",{bubbles:true,data:"中"})));await editor().press("Shift+Enter");assert.match(await editor().inputValue(),/\n/)
  await editor().fill("sent once");await editor().press("Enter");await messageText("sent once").waitFor()
  assert.equal((await log()).filter(r=>r.method==="send").length,1)
  await editor().fill("unknown");await editor().press("Enter");await page.waitForFunction(()=>document.querySelector(".pa-editor textarea")?.disabled===true)
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
  await page.waitForFunction(()=>document.activeElement?.classList.contains("pa-orb"))
  await controlledRaces()
  all.push(...await log());return ["600-event bounded initial100 / frontier602 / events603-604","opaque history and scroll anchor","old-generation work/event versus delayed older page","101 permission and trace summaries via manual more","detail-only answers / typed oversized detail","new generation cancels ignored-abort details","snapshot errors retain window and draft","same-account/Bot recovery latch across return and host remount","manual snapshot is the only recovery unlock","new account isolation","queued16 and unknown17","ignored-abort mutation preflight and source revocation","API58 metadata / typed unsupported view / no fallback","IME, quote, draft switch, one send, unknown intent after remount","focus and voice ownership / no horizontal overflow","second permission preflight cursor/generation recovery latch","all parallel mutation controls abort on recovery/unmount and cannot resume after same-fence unlock"]
}
let forbidden=0
const server=createServer((req,res)=>{const url=new URL(req.url,"http://fixture");if(url.pathname.startsWith("/v1")){forbidden++;res.writeHead(500);res.end("No live API in fixture");return}const text=url.pathname==="/"?html:content.get(url.pathname);if(text===undefined){res.writeHead(404);res.end();return}res.setHeader("Content-Type",url.pathname.endsWith(".js")?"text/javascript":url.pathname.endsWith(".css")?"text/css":"text/html");res.end(text)})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,results=[]
const failures=[]
try {
  for(const engine of [chromium,webkit].filter(e=>!process.env.BOTS_ENGINE||e.name()===process.env.BOTS_ENGINE))for(const width of (process.env.BOTS_WIDTH?[Number(process.env.BOTS_WIDTH)]:[390,1440]))for(const language of (process.env.BOTS_LANGUAGE?[process.env.BOTS_LANGUAGE]:["zh-Hant","en"])) {
    const w=language==="en"?{allBots:"All Bots",select:"Select a Bot to start reading",more:"Load more",message:"Message",back:"Back to Bots",permission:"Permission request",always:"Always allow (unavailable)",allow:"Allow once",unknown:"Outcome unknown; no automatic retry",create:"Create Bot",name:"Name",submit:"Create",sessions:"Back to Sessions",earlier:"Earlier messages",refresh:"Check for updates"}:{allBots:"所有 Bots",select:"選擇一位 Bot 開始閱讀",more:"載入更多",message:"訊息",back:"返回 Bots",permission:"權限請求",always:"一律允許（不提供）",allow:"允許這次",unknown:"結果未知；不會自動重送",create:"新增 Bot",name:"名稱",submit:"建立",sessions:"返回 Sessions",earlier:"較早的訊息",refresh:"檢查新訊息"}
    const browser=await engine.launch({headless:true}),name=`${engine.name()}-${width}-${language}`
    const page=await browser.newPage({viewport:{width,height:width===1440?960:844},reducedMotion:"reduce"})
    const consoleErrors=[];page.on("pageerror",error=>consoleErrors.push(error.message))
    await page.route("**/*",route=>{const url=new URL(route.request().url());if(url.origin!==origin||url.pathname.startsWith("/v1")){forbidden++;return route.abort()}return route.continue()})
    const shot=async code=>page.screenshot({path:path.join(artifacts,`${code}-${name}.png`),fullPage:true})
    const log=()=>page.evaluate(()=>window.botsFixture.log)
    const row=(bot="Project assistant",machine="studio")=>page.getByRole("button",{name:new RegExp(`^${bot}, ${machine},`)})
    try {
      if(process.env.BOTS_CATALOG_ONLY){
        const checks=await catalogChecks(page,origin,language,shot,log)
        assert.deepEqual(consoleErrors,[])
        await writeFile(path.join(artifacts,`interactions-${name}.json`),JSON.stringify({log:page.catalogRequests,consoleErrors,checks,modelInvocations:0},null,2))
        results.push({engine:engine.name(),width,language,status:"pass",checks});continue
      }
      const checks=await boundedChecks(page,origin,language,width,shot,log)
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
