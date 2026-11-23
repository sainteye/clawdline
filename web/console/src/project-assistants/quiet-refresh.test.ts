import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import {followQuietRefresh} from "./quiet-refresh.ts"

const flush=()=>new Promise<void>(r=>setImmediate(r))
class FakeDocument extends EventTarget {
  visibilityState="visible"
  set(state:string){this.visibilityState=state;this.dispatchEvent(new Event("visibilitychange"))}
}
function slowLoad(){
  let calls=0,active=0,max=0
  const releases:(()=>void)[]=[]
  const load=async()=>{calls++;active++;max=Math.max(max,active);await new Promise<void>(r=>releases.push(r));active--}
  return {load,get calls(){return calls},get max(){return max},release(){releases.shift()?.()}}
}

test("a slow list read is never overlapped by its timer or by an external notice",async t=>{
  t.mock.timers.enable({apis:["setTimeout","setInterval"]})
  const doc=new FakeDocument(),win=new EventTarget(),control=new AbortController(),reads=slowLoad()
  const notify=followQuietRefresh(reads.load,control.signal,3000,doc,win)
  assert.equal(reads.calls,1)
  t.mock.timers.tick(9000);notify();notify()
  assert.equal(reads.calls,1,"no second request while the first is in flight")
  control.abort()
})

test("a hidden page stops reading and the foreground reads at once",async t=>{
  t.mock.timers.enable({apis:["setTimeout","setInterval"]})
  const doc=new FakeDocument(),win=new EventTarget(),control=new AbortController()
  let calls=0
  const notify=followQuietRefresh(async()=>{calls++},control.signal,3000,doc,win)
  assert.equal(calls,1)
  doc.set("hidden")
  t.mock.timers.tick(60_000);notify()
  assert.equal(calls,1,"hidden: neither the fallback timer nor a notice reads")
  doc.set("visible");await flush()
  assert.equal(calls,2,"returning to the foreground reads immediately")
  control.abort()
})

test("a notice reads now and resets the fallback cadence",async t=>{
  t.mock.timers.enable({apis:["setTimeout","setInterval"]})
  const doc=new FakeDocument(),win=new EventTarget(),control=new AbortController()
  let calls=0
  const notify=followQuietRefresh(async()=>{calls++},control.signal,10_000,doc,win)
  assert.equal(calls,1)
  notify();await flush();assert.equal(calls,2,"an external notice triggers a read")
  control.abort()
  t.mock.timers.tick(60_000);notify()
  assert.equal(calls,2,"an aborted follower never reads again")
})
