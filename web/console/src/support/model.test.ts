import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's test runner loads TypeScript source.
import { helpRoute,helpAccess,translationState,conversationView,githubAvatarSource,type Message,type Article } from "./model.ts"
test("help routes remain independent of a machine or pairing",()=>{
 assert.deepEqual(helpRoute("/help/questions/sq_one"),{page:"thread",id:"sq_one"})
 assert.deepEqual(helpRoute("/help/admin/faq/new"),{page:"editor",id:"new"})
 assert.equal(helpRoute("/help/admin/worker").page,"worker")
})
test("only a canonical GitHub avatar can cause an external image request",()=>{
 const safe="https://avatars.githubusercontent.com/u/583231?s=80"
 assert.equal(githubAvatarSource(safe),safe)
 for(const value of [undefined,null,{},"javascript:alert(1)","//avatars.githubusercontent.com/u/1?s=80","http://avatars.githubusercontent.com/u/1?s=80","https://avatars.githubusercontent.com.evil.test/u/1?s=80","https://"+["fixture-user","avatars.githubusercontent.com/u/1?s=80"].join("@"),"https://avatars.githubusercontent.com/u/1?s=80&tracking=private",safe+"\n","https://avatars.githubusercontent.com/u/0?s=80","https://avatars.githubusercontent.com/u/99999999999999999999?s=80"]) assert.equal(githubAvatarSource(value),null)
})
test("a published older translation is stale when the source changes",()=>{
 const text={title:"FAQ",body:"Answer",slug:"faq",revision:1,source_revision:1,sources:[],updated_at:"2026-01-01T00:00:00Z"}
 const article:Article={_id:"faq_example",revision:2,source_locale:"en",source_revision:1,topic:"cloud",source_question_id:null,translations:{"zh-Hant":{draft:text,published:text}}}
 assert.equal(translationState(article,"zh-Hant"),"published");assert.equal(translationState(article,"ja"),"missing")
 article.source_revision=2;assert.equal(translationState(article,"zh-Hant"),"stale")
 article.translations["zh-Hant"]!.draft={...text,source_revision:2,revision:2};assert.equal(translationState(article,"zh-Hant"),"draft")
})

test("management deep links require an administrator while members keep their own threads",()=>{
 for (const path of ["/help/admin", "/help/admin/questions/sq_example", "/help/admin/faq", "/help/admin/faq/new", "/help/admin/worker"]) assert.equal(helpAccess(helpRoute(path)),"admin",path)
 for (const path of ["/help/ask", "/help/questions", "/help/questions/sq_example"]) assert.equal(helpAccess(helpRoute(path)),"member",path)
 for (const path of ["/help", "/help/faq/faq_example"]) assert.equal(helpAccess(helpRoute(path)),"public",path)
})

test("newest replies lead the conversation without changing chronological read receipts",()=>{
 const message=(id:string,kind:Message["kind"]):Message=>({id,kind,body:id,locale:"en",original_language:"en",at:"2026-01-01T00:00:00Z",sources:[]})
 const history=[message("question","member"),message("first","agent"),message("correction","operator"),message("followup","member")]
 const view=conversationView(history)
 assert.deepEqual(view.messages.map(m=>m.id),["followup","correction","first","question"])
 assert.equal(view.latestReplyID,"correction")
 assert.equal(history.at(-1)?.id,"followup")
 assert.deepEqual(history.map(m=>m.id),["question","first","correction","followup"])
 assert.equal(conversationView([message("question","member")]).latestReplyID,null)
 assert.deepEqual(conversationView(),{messages:[],latestReplyID:null})
})
