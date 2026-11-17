import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import {cleanRoutineTrigger,newRoutineTrigger,routineEventSources,validRoutineTrigger} from "./routine-events.ts"

test("switching trigger kinds drops event fields and calendar fields",()=>{
  const event={...newRoutineTrigger("event"),event:"issues.opened",filters:[{key:"repo",value:"synthetic/project"}]}
  for(const type of ["interval","once","cron","webhook"]){
    const next=cleanRoutineTrigger({...event,...newRoutineTrigger(type)})
    assert.deepEqual(Object.keys(next),["type","seconds","at","expression","timeZone"])
    assert.equal(next.source,undefined);assert.equal(next.filters,undefined)
  }
  const extended={...event,unknown:"never sent"}
  const wire=cleanRoutineTrigger(extended)
  assert.equal("unknown" in wire,false)
  event.filters[0].value="changed draft"
  assert.equal(wire.filters![0].value,"synthetic/project")
})

test("event validation uses exact sources, UTF-8 bounds and unique keys",()=>{
  for(const source of routineEventSources)assert.equal(validRoutineTrigger({...newRoutineTrigger("event"),source}),true)
  const valid={...newRoutineTrigger("event"),event:"界".repeat(66)+"ab",filters:[{key:"界".repeat(66)+"ab",value:"界".repeat(1333)+"a"}]}
  assert.equal(validRoutineTrigger(valid),true)
  for(const change of [{source:"GitHub"},{event:valid.event+"a"},{event:""},{event:"\ud800"},{filters:[{key:"",value:""}]},{filters:[{key:"x",value:""},{key:"x",value:"different"}]},{filters:[{key:"x",value:"界".repeat(1334)}]},{filters:Array.from({length:21},(_,n)=>({key:String(n),value:""}))},{seconds:1},{future:"field"}])assert.equal(validRoutineTrigger({...valid,...change}),false)
  assert.equal(validRoutineTrigger(newRoutineTrigger("event"),false),false)
  assert.equal(validRoutineTrigger(newRoutineTrigger("webhook"),false),false)
  assert.equal(validRoutineTrigger(newRoutineTrigger(),false),true)
  assert.equal(validRoutineTrigger({...newRoutineTrigger(),source:"github"}),false)
})
