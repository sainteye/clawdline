import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's test runner loads TypeScript source.
import { helpRoute,translationState,type Article } from "./model.ts"
test("help routes remain independent of a machine or pairing",()=>{
 assert.deepEqual(helpRoute("/help/questions/sq_one"),{page:"thread",id:"sq_one"})
 assert.deepEqual(helpRoute("/help/admin/faq/new"),{page:"editor",id:"new"})
 assert.equal(helpRoute("/help/admin/worker").page,"worker")
})
test("a published older translation is stale when the source changes",()=>{
 const text={title:"FAQ",body:"Answer",slug:"faq",revision:1,source_revision:1,sources:[],updated_at:"2026-01-01T00:00:00Z"}
 const article:Article={_id:"faq_example",revision:2,source_locale:"en",source_revision:1,topic:"cloud",source_question_id:null,translations:{"zh-Hant":{draft:text,published:text}}}
 assert.equal(translationState(article,"zh-Hant"),"published");assert.equal(translationState(article,"ja"),"missing")
 article.source_revision=2;assert.equal(translationState(article,"zh-Hant"),"stale")
 article.translations["zh-Hant"]!.draft={...text,source_revision:2,revision:2};assert.equal(translationState(article,"zh-Hant"),"draft")
})
