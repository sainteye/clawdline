import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner needs source extensions.
import { dashboardFetch } from "./dashboard-reader.ts"
// @ts-expect-error -- Node's strip-types runner needs source extensions.
import { RelayReader, type CloudReadClient } from "./relay-reader.ts"

test("inspecting another machine reads its usage without repointing the active reader", async () => {
  const asks: { machine: string; word: string; kind: string }[] = []
  const untouched = {
    events: () => () => undefined,
    sessions: async () => { throw new Error("usage inspection must not read the active list") },
    transcript: async () => { throw new Error("usage inspection must not read a transcript") },
  }
  const client: CloudReadClient = { ...untouched, ready: true, _machineRequest: async (machine, word, _body, kind) => {
    asks.push({ machine, word, kind })
    return { cpu_percent: machine === "machine-b" ? 20 : 10 }
  } }
  const active = new RelayReader("machine-a")
  active.attach(client)
  const inspect = dashboardFetch("machine-b", () => client)
  assert.equal((await (await inspect("/v1/machine/usage")).json()).cpu_percent, 20)
  assert.equal(active.machine, "machine-a")
  assert.equal(active.log.length, 0)
  assert.deepEqual(asks, [{ machine: "machine-b", word: "machine-usage", kind: "read" }])
})

test("an open inspection follows replacement Cloud clients while keeping the selected machine", async () => {
  const asks: string[] = []
  const make = (label: string): CloudReadClient => ({ ready: true,
    events: () => () => undefined,
    sessions: async () => { throw new Error("no list read expected") },
    transcript: async () => { throw new Error("no transcript read expected") }, _machineRequest: async (machine) => {
    asks.push(label + ":" + machine); return { cpu_percent: 5 }
  } })
  let connected = make("first")
  const inspect = dashboardFetch("machine-b", () => connected)
  await inspect("/v1/machine/usage")
  connected = make("renewed")
  await inspect("/v1/machine/usage")
  assert.deepEqual(asks, ["first:machine-b", "renewed:machine-b"])
})

test("API inspection pins the selected machine, carries its exact window and caller cancellation", async () => {
  const asks: {machine:string;word:string;body:Record<string,unknown>;signal:AbortSignal|undefined}[]=[]
  const client: CloudReadClient = {ready:true,events:()=>()=>undefined,
    sessions:async()=>{throw new Error("no active list read")},transcript:async()=>{throw new Error("no transcript read")},
    _machineRequest:async(machine,word,body,_kind,_timeout,options)=>{
      asks.push({machine,word,body,signal:options?.signal});return {source:"owner_local",build:machine}
    }}
  const inspect=dashboardFetch("linux-b",()=>client),ctl=new AbortController()
  const response=await inspect("/v1/api-performance?hours=24&until=1793179200000",{signal:ctl.signal})
  assert.equal(response.status,200)
  assert.equal(response.headers.get("Cache-Control"),"no-store")
  assert.equal((await response.json()).build,"linux-b")
  assert.deepEqual(asks,[{machine:"linux-b",word:"api-performance",body:{hours:"24",until:1793179200000},signal:ctl.signal}])
  for(const query of ["hours=2","hours=1&hours=6","hours=1&account=other","until=-1","until=1.5"]){
    assert.equal((await inspect("/v1/api-performance?"+query)).status,400)
  }
  assert.equal(asks.length,1)
  assert.equal((await inspect("/su/v1/health/performance")).status,501)
})
