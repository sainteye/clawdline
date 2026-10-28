import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node runs TypeScript source directly.
import { shareMetadata, shareText, metadataHTML, metadataFields, SHARE_IMAGE } from "./share-metadata.ts"
import { publicResource, requestLocale, resolveMetadata } from "../../edge/worker.mjs"
const id="sq_"+"a".repeat(40),path="/community/topics/"+id
const json=(v:unknown,status=200)=>new Response(JSON.stringify(v),{status,headers:{"Content-Type":"application/json"}})
test("public list metadata has distinct titles, descriptions, fixed canonical and large image",()=>{
 for(const [path,title] of [["questions","使用問答"],["issues","Issues"],["ideas","Ideas"]]) {
  const m=shareMetadata("https://untrusted.invalid/community/"+path+"/?tracking=1#reply","zh-Hant")
  assert.ok(m.title.startsWith(title));assert.ok(m.description);assert.equal(m.canonical,"https://app.clawdline.com/community/"+path);assert.equal(m.image,SHARE_IMAGE);assert.equal(m.robots,"index, follow")
  assert.ok(metadataFields(m).some(([key,n,v])=>key==="name"&&n==="twitter:card"&&v==="summary_large_image"))
 }
 assert.equal(shareMetadata("/community/").canonical,"https://app.clawdline.com/community/issues")
 assert.match(shareMetadata("/community/questions","ja").title,/Usage questions/)
})
test("a public topic uses its original text, escaped markup and original language",()=>{
 const m=shareMetadata(path,"en",{title:'設定 <script>alert(1)</script> "A&B"',body:'## 操作\n**開啟** [文件](https://example.com) ![私密圖片標記](https://example.com/i.png)',locale:"zh-Hant"})
 assert.equal(m.type,"article");assert.equal(m.locale,"zh_TW");assert.equal(m.description,"操作 開啟 文件")
 const html=metadataHTML(m);assert.ok(!html.includes("<script>"));assert.ok(html.includes("&quot;A&amp;B&quot;"));assert.equal((html.match(/<title>/g)??[]).length,1)
 assert.equal(shareText("😀".repeat(200)).length,320);assert.ok(!shareText("😀".repeat(200)).includes("�"))
})
test("private and loading destinations never present a topic or personal identity",()=>{
 for(const p of ["/community/mine","/community/new","/help/admin","/help/admin/questions/secret","/help/questions/secret",path]) {
  const m=shareMetadata(p,"en");assert.equal(m.robots,"noindex, nofollow");assert.ok(!m.description.includes("secret"));assert.equal(m.type,"website")
 }
 assert.equal(publicResource("/help/admin/questions/"+id,"en"),null)
 assert.equal(publicResource("/community/topics/../../v1/auth/session","en"),null)
 assert.equal(publicResource("/community/topics/not-valid","en"),null)
})
test("edge lookup fetches only anonymous public content and drops caller credentials and queries",async()=>{
 const req=new Request("https://app.clawdline.com"+path+"?token=private",{headers:{Cookie:"private",Authorization:"secret","Accept-Language":"en;q=0.3,zh-TW;q=1"}})
 let calls=0
 const m=await resolveMetadata(req,async(url:RequestInfo|URL,init?:RequestInit)=>{calls++;assert.equal(url,"https://api.clawdline.com/v1/community/topics/"+id);assert.deepEqual(init!.headers,{Accept:"application/json"});assert.equal(init!.credentials,"omit");assert.equal(init!.redirect,"manual");return json({topic:{id,title:"操作入口",source_language:"zh-Hant",messages:[{body:"從側邊欄開啟。"},{body:"不得選最新回覆作為原文"}],reporter:{email:"must-not-leak"}}})})
 assert.equal(calls,1);assert.equal(m.title,"操作入口 | Clawdline");assert.equal(m.description,"從側邊欄開啟。");assert.ok(!metadataHTML(m).includes("must-not-leak"))
 let privateCalls=0
 for(const p of ["/community/mine","/help/admin/worker"])await resolveMetadata(new Request("https://app.clawdline.com"+p),async()=>{privateCalls++;throw new Error("must not fetch")})
 assert.equal(privateCalls,0)
 assert.equal(requestLocale("en;q=1,zh-TW;q=0.2"),"en");assert.equal(requestLocale("zh-TW;q=0,ja;q=1"),"en")
})
test("deleted, private, malformed, oversized and failed upstream replies use safe uncached fallback",async()=>{
 const request=new Request("https://app.clawdline.com"+path)
 for(const answer of [()=>json({error:"private content"},404),()=>new Response(null,{status:302,headers:{Location:"https://untrusted.invalid"}}),()=>json({topic:{id:"wrong",title:"secret",messages:[{body:"secret"}]}}),()=>new Response("not json",{headers:{"Content-Type":"text/html"}}),()=>new Response(" ".repeat(1_048_577),{headers:{"Content-Type":"application/json"}}),()=>{throw new Error("offline")}]) {
  const m=await resolveMetadata(request,async()=>answer());assert.equal(m.robots,"noindex, nofollow");assert.ok(!metadataHTML(m).includes("secret"))
 }
})
test("a bounded upstream timeout still lets the page open with generic metadata",async()=>{
 const m=await resolveMetadata(new Request("https://app.clawdline.com"+path),(_url:RequestInfo|URL,init?:RequestInit)=>new Promise((_resolve,reject)=>init!.signal!.addEventListener("abort",()=>reject(new Error("timeout")),{once:true})))
 assert.equal(m.robots,"noindex, nofollow")
})
test("FAQ previews use only the public article and requested supported locale",async()=>{
 const m=await resolveMetadata(new Request("https://app.clawdline.com/help/faq/faq-example",{headers:{"Accept-Language":"zh-TW"}}),async(url:RequestInfo|URL)=>{assert.equal(url,"https://api.clawdline.com/v1/faq/faq-example?locale=zh-Hant");return json({article:{id:"faq-example",title:"如何設定",body:"請先開啟主控台。",locale:"zh-Hant"}})})
 assert.equal(m.title,"如何設定 | Clawdline");assert.equal(m.robots,"index, follow")
})
