import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { ProjectAssistantSourceClient, capturedAssistantRelayFetch, modelCatalogRelayRequest, type AssistantGrant } from "./project-assistants.ts"
// @ts-expect-error -- Node executes source extensions.
import { CARRIED } from "./carry-generated.ts"

// The phone failure of 2026-11-25: Bots settings over Cloud chose Codex and its
// model list never arrived. The Bots source now reads the machine's catalog
// through the relay word model-catalog, with no Bot named.
const grant = (apiLevel: number): AssistantGrant => ({accountKey:"synthetic",readerID:"reader",machineID:"m1",
  authorizationRevision:"grant",apiLevel,paired:true,online:true,writable:false})
const codex = {agent:"codex",models:[{value:"model-sol",name:"6.2 Sol"}],default_name:"6.2 Sol",efforts:["high","xhigh"],source:"agent",observed_at:"2026-11-25T00:00:00Z",refreshing:false}

test("a phone's Bots settings read Codex's models through the model-catalog word", async () => {
  const calls: unknown[] = []
  const connection = {
    machineDescriptor: () => ({machine:{commands:["model-catalog"]}}),
    async _machineRequest(machine: string, word: string, fields: Record<string, unknown>) { calls.push({machine,word,fields}); return {agents:[codex]} },
  }
  const g = grant(120)
  const source = new ProjectAssistantSourceClient(g, capturedAssistantRelayFetch("m1",()=>connection,Object.keys(CARRIED)), ()=>g)
  assert.equal(source.canModelCatalog(), true)
  assert.deepEqual(await source.modelCatalog("codex", new AbortController().signal), {agents:[codex]})
  assert.deepEqual(await source.modelCatalog(undefined, new AbortController().signal), {agents:[codex]})
  assert.deepEqual(calls, [{machine:"m1",word:"model-catalog",fields:{agent:"codex"}},{machine:"m1",word:"model-catalog",fields:{}}])
})

test("a machine older than the catalog is not asked", async () => {
  const g = grant(119)
  const source = new ProjectAssistantSourceClient(g, capturedAssistantRelayFetch("m1",()=>null,Object.keys(CARRIED)), ()=>g)
  assert.equal(source.canModelCatalog(), false)
  await assert.rejects(source.modelCatalog(undefined, new AbortController().signal))
})

test("only GET /v1/model-catalog with at most one agent is the word", () => {
  assert.equal(modelCatalogRelayRequest("/v1/projects"), null)
  assert.deepEqual(modelCatalogRelayRequest("/v1/model-catalog?agent=grok"), {word:"model-catalog",kind:"read",fields:{agent:"grok"},id:""})
  assert.throws(() => modelCatalogRelayRequest("/v1/model-catalog?model=x"))
  assert.throws(() => modelCatalogRelayRequest("/v1/model-catalog", {method:"POST"}))
})
