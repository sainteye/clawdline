import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const {outputFiles}=await build({stdin:{contents:'export {LazyEntryBody,readStoredEntry} from "./LazyEntryBody.tsx"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
const compiled={exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {LazyEntryBody,readStoredEntry}=compiled.exports
const reference="0:"+"a".repeat(64)
test("a folded tool body keeps its metadata without fetching or rendering stored text",()=>{
 const saved=globalThis.fetch
 globalThis.fetch=()=>assert.fail("collapsed body was fetched")
 try {
  const html=renderToStaticMarkup(React.createElement(LazyEntryBody,{sessionID:"session",detail:{reference,textBytes:2000,lineCount:20},open:false,preview:React.createElement("span",null,"file.go +20"),children:()=>assert.fail("stored body rendered before read")}))
  assert.match(html,/file.go \+20/)
  assert.doesNotMatch(html,/loading|<pre/)
 }finally{globalThis.fetch=saved}
})
test("expanding reads only the named Session and content version with cancellation",async()=>{
 const saved=globalThis.fetch,controller=new AbortController()
 globalThis.fetch=async(path,options)=>{
  const url=new URL(path,"https://fixture")
  assert.equal(url.searchParams.get("session"),"session/one")
  assert.match(url.searchParams.get("view"),new RegExp("^entry-v1z?:"+reference+"$"))
  assert.equal(options.signal,controller.signal)
  assert.equal(options.cache,"no-store")
  return Response.json({reference,entry:{role:"tool",text:"complete text"}})
 }
 try {assert.equal((await readStoredEntry("session/one",reference,controller.signal)).text,"complete text")}
 finally{globalThis.fetch=saved}
})
test("another reference or an incomplete answer cannot populate an expanded tool",async()=>{
 const saved=globalThis.fetch
 try {
  for(const answer of [{reference:"other",entry:{role:"tool",text:"wrong"}},{reference,entry:{role:"assistant",text:"wrong"}},{reference,entry:{role:"tool",text:"preview",detail:{reference}}},{reference,entry:{role:"tool",text:"bad patch",fileChanges:[{path:7}]}}]) {
   globalThis.fetch=async()=>Response.json(answer)
   await assert.rejects(readStoredEntry("s",reference,new AbortController().signal),error=>error.code==="bad_payload")
  }
  globalThis.fetch=async()=>Response.json({error:"tool_result_stale",detail:"Read the conversation again"},{status:409})
  await assert.rejects(readStoredEntry("s",reference,new AbortController().signal),error=>error.code==="tool_result_stale")
 }finally{globalThis.fetch=saved}
})


test("a patch expansion cannot silently accept an answer with no patch body",async()=>{
 const saved=globalThis.fetch
 globalThis.fetch=async()=>Response.json({reference,entry:{role:"tool",text:"missing change"}})
 try{await assert.rejects(readStoredEntry("s",reference,new AbortController().signal,true),error=>error.code==="bad_payload")}
 finally{globalThis.fetch=saved}
})

test("cancelling an unfinished expansion stops that request without retrying",async()=>{
 const saved=globalThis.fetch,controller=new AbortController()
 let requests=0
 globalThis.fetch=async(_path,{signal})=>{
  requests++
  return new Promise((_resolve,reject)=>signal.addEventListener("abort",()=>reject(new DOMException("closed","AbortError")),{once:true}))
 }
 try {
  const reading=readStoredEntry("s",reference,controller.signal)
  controller.abort()
  await assert.rejects(reading,error=>error.name==="AbortError")
  assert.equal(requests,1)
 }finally{globalThis.fetch=saved}
})
