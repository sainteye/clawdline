import test from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import vm from "node:vm"
function worker(windows=[]) {
  const handlers={},shown=[],opened=[],focused=[]
  const self={location:{origin:"https://app.clawdline.com"},addEventListener:(name,handler)=>{handlers[name]=handler},skipWaiting(){},registration:{showNotification:async(title,options)=>{shown.push({title,...options})}},clients:{claim:async()=>{},matchAll:async()=>windows,openWindow:async url=>{opened.push(url)}}}
  vm.runInNewContext(readFileSync(new URL("../../public/support-sw.js",import.meta.url),"utf8"),{self,URL})
  async function fire(name,extra){let wait;handlers[name]({...extra,waitUntil:p=>{wait=p}});await wait}
  return{fire,shown,opened,focused}
}
test("new-question push displays generic content and retains the exact Admin destination",async()=>{
 const w=worker(),url="https://app.clawdline.com/help/admin/questions/sq_"+"a".repeat(40)
 await w.fire("push",{data:{json:()=>({web_push:8030,notification:{title:"New question",body:"Open the question",navigate:url,tag:"one"}})}})
 assert.equal(w.shown[0].title,"New question");assert.equal(w.shown[0].data.url,url)
})
test("click opens the Admin question even when a Session or member page is already open",async()=>{
 const w=worker([{url:"https://app.clawdline.com/#session",focus:async()=>{throw new Error("wrong page must not consume click")}}]),url="https://app.clawdline.com/help/admin/questions/sq_"+"b".repeat(40)
 await w.fire("notificationclick",{notification:{data:{url},close(){}}})
 assert.deepEqual(w.opened,[url])
})
test("external, credential-bearing and non-Admin destinations never leave the inbox",async()=>{
 for(const url of ["https://evil.test/help/admin","/help/questions/sq_"+"c".repeat(40),"javascript:alert(1)","https://someone@example.invalid/help/admin","/help/admin?token=secret"]){
  const w=worker();await w.fire("push",{data:{json:()=>({notification:{navigate:url}})}})
  assert.equal(w.shown[0].data.url,"https://app.clawdline.com/help/admin")
 }
})
test("an already open exact Admin detail is focused without opening a duplicate",async()=>{
 const url="https://app.clawdline.com/help/admin/questions/sq_"+"d".repeat(40);let focused=0
 const w=worker([{url,focus:async()=>{focused++}}]);await w.fire("notificationclick",{notification:{data:{url},close(){}}})
 assert.equal(focused,1);assert.equal(w.opened.length,0)
})
test("monitoring notifications retain only the exact same-origin performance destination",async()=>{
 const url="https://app.clawdline.com/admin/health/services?view=performance",w=worker()
 await w.fire("push",{data:{json:()=>({notification:{navigate:url}})}})
 assert.equal(w.shown[0].data.url,url)
 for(const denied of [url+"&token=secret",url+"#private","https://evil.test/admin/health/services?view=performance"]){
  const other=worker();await other.fire("push",{data:{json:()=>({notification:{navigate:denied}})}})
  assert.equal(other.shown[0].data.url,"https://app.clawdline.com/help/admin")
 }
})
