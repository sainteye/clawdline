import { test } from "node:test"
import assert from "node:assert/strict"
import { CloudClient } from "../legacy/js/net/cloud-client.js"
import { importMasterSecret, importSenderPublicKey, openEnvelope, sealEnvelope } from "../legacy/js/net/cloud-crypto.js"
// @ts-expect-error Node executes TypeScript directly.
import { ViewerRekey, VIEWER_REKEY_COOLDOWN_MS } from "./viewer-rekey.ts"
// @ts-expect-error Node executes TypeScript directly.
import { sealRekeyAnswer, rekeyKeyID, rekeyCanonical, openRekeyAnswer } from "./rekey-crypto.ts"
// @ts-expect-error Node executes TypeScript directly.
import { storedPairing } from "./viewer-rekey-storage.test.ts"

// Recorded AccountDO control shapes; operation identifiers are anonymized.
const recordedRouted = '{"type":"content_key_routed","frame":"content_key_offer","operation_id":"recorded-route-operation","delivered":1}'
const recordedError = '{"type":"content_key_error","frame":"content_key_offer","code":"content_key_no_target","operation_id":"recorded-error-operation"}'

test("recorded relay controls do not adopt; exact offer errors release pending and preserve cooldown", async () => {
 const s = await scenario()
 try {
  await s.client.viewerRekey.request(s.pairing)
  const operation = s.sent.find(f=>f.type==="content_key_offer").operation_id
  await s.client._receive(recordedRouted)
  await s.client._receive(JSON.stringify({...JSON.parse(recordedRouted),operation_id:operation}))
  assert.equal(s.client.viewerRekey.pendingCount,1)
  assert.equal((await s.session.machinePairing("machine-test")).keyID,"ms-1")
  assert.equal(s.client.machinePairings.get("machine-test"),undefined)
  await s.client._receive(recordedError) // recorded, unrelated operation
  assert.equal(s.client.viewerRekey.pendingCount,1)
  const matching = {...JSON.parse(recordedError),operation_id:operation}
  await s.client._receive(JSON.stringify({type:"content_key_error",frame:"content_key_offer",code:"content_key_malformed"}))
  assert.equal(s.client.viewerRekey.pendingCount,1)
  for (const wrong of [{operation_id:"other"},{frame:"content_key_answer"}]) {
   await s.client._receive(JSON.stringify({...matching,...wrong}))
   assert.equal(s.client.viewerRekey.pendingCount,1)
  }
  for (const wrong of [{code:"secret raw error"},{operation_id:42},{operation_id:""},{frame:[]},{extra:"secret"}]) {
   await assert.rejects(s.client._receive(JSON.stringify({...matching,...wrong})), {code:"bad_frame"})
   assert.equal(s.client.viewerRekey.pendingCount,1)
  }
  await assert.rejects(s.client._receive(JSON.stringify(matching)), {code:"cloud_reconnecting",message:"content key exchange rejected"})
  assert.equal(s.client.viewerRekey.pendingCount,0)
  assert.equal(await s.client.viewerRekey.request(s.pairing),false)
  assert.equal(s.sent.filter(f=>f.type==="content_key_offer").length,1)
  s.clock.now += VIEWER_REKEY_COOLDOWN_MS
  assert.equal(await s.client.viewerRekey.request(s.pairing),true)
  await s.client._receive(JSON.stringify(matching)) // stale refusal cannot cancel the next operation
  assert.equal(s.client.viewerRekey.pendingCount,1)
 } finally {s.client.stop()}
})

test("relay cancellation during adoption invalidates durable authority and never updates memory", async () => {
 const s=await scenario()
 let release!:()=>void, entered!:()=>void, valid!:()=>boolean, adopted=0
 const waiting=new Promise<void>(resolve=>{entered=resolve}), barrier=new Promise<void>(resolve=>{release=resolve})
 let offer=""
 const rekey=new ViewerRekey({account:s.authority.account_id,viewer:s.authority.viewer_device_id,
  signing:s.viewer.privateKey,viewerKey:s.viewerKey,now:()=>s.clock.now,send:raw=>{offer=raw},
  persist:async(pairing,state,operation,check)=>{valid=check;entered();await barrier;return s.session.persistMachineRekey(pairing,state,operation,check)},
  adopted:()=>{adopted++}})
 try {
  await rekey.request(s.pairing)
  const answering=rekey.answer(await s.answer(offer))
  await waiting
  assert.throws(()=>rekey.control({...JSON.parse(recordedError),operation_id:JSON.parse(offer).operation_id}),{code:"cloud_reconnecting"})
  assert.equal(valid(),false)
  release()
  await assert.rejects(answering) // The synthetic database reports its abort as a transaction failure.
  assert.equal((await s.session.machinePairing("machine-test")).keyID,"ms-1")
  assert.equal(adopted,0)
  assert.equal(await rekey.request(s.pairing),false)
 } finally {release?.();rekey.cancel();s.client.stop()}
})

test("relay cancellation after a durable commit cannot adopt into memory", async () => {
 const s=await scenario()
 let release!:()=>void, entered!:()=>void, adopted=0, offer=""
 const waiting=new Promise<void>(resolve=>{entered=resolve}), barrier=new Promise<void>(resolve=>{release=resolve})
 const rekey=new ViewerRekey({account:s.authority.account_id,viewer:s.authority.viewer_device_id,
  signing:s.viewer.privateKey,viewerKey:s.viewerKey,now:()=>s.clock.now,send:raw=>{offer=raw},
  persist:async(pairing,state,operation,check)=>{
   const saved=await s.session.persistMachineRekey(pairing,state,operation,check)
   entered();await barrier;return saved
  },adopted:()=>{adopted++}})
 try {
  await rekey.request(s.pairing)
  const answering=rekey.answer(await s.answer(offer))
  await waiting
  assert.throws(()=>rekey.control({...JSON.parse(recordedError),operation_id:JSON.parse(offer).operation_id}),{code:"cloud_reconnecting"})
  release()
  assert.equal(await answering,false)
  assert.equal(adopted,0)
  assert.equal((await s.session.machinePairing("machine-test")).epoch,2,"already authorized durable commit is retained")
 } finally {release?.();rekey.cancel();s.client.stop()}
})

async function scenario(options: {enabled?:boolean; writes?:boolean; auto?:boolean} = {}) {
 const saved = await storedPairing(), viewer = await crypto.subtle.generateKey("Ed25519",false,["sign","verify"])
 const viewerKey = new Uint8Array(await crypto.subtle.exportKey("raw",viewer.publicKey))
 const authority = {account_id:"account-test",machine_id:"machine-test",viewer_device_id:"viewer-test",activeViewerKey:viewerKey}
 const clock = {now:1700000000000}, sent: any[] = [], errors: any[] = [], answers: string[] = []
 const key = crypto.getRandomValues(new Uint8Array(32)), master = await importMasterSecret(key)
 const state = {epoch:2,key_id:await rekeyKeyID(2,key),key}
 let socket: any, seq = 0
 async function answer(raw: string, overrides: any = {}) {
  const ephemeral = await crypto.subtle.generateKey("X25519",true,["deriveBits"]) as CryptoKeyPair
  return sealRekeyAnswer(raw,{...authority,...overrides},state,saved.machine.privateKey,
   ephemeral.privateKey,crypto.getRandomValues(new Uint8Array(12)),clock.now)
 }
 class Socket {
  readyState = 1; onmessage: any; onclose: any; onerror: any
  constructor() { socket = this }
  send(raw: string) {
   const frame = JSON.parse(raw); sent.push(frame)
   if(frame.type === "content_key_offer" && options.auto) {
    void answer(raw).then(a => {answers.push(a); this.onmessage({data:a})})
   }
  }
  close() { this.readyState = 3 }
 }
 const client: any = new CloudClient({relayURL:"wss://relay.example.test",deviceToken:"synthetic",
  account:authority.account_id,deviceID:authority.viewer_device_id,devicePrivateKey:viewer.privateKey,
  devicePublicKey:viewerKey,enableViewerRekey:options.enabled ?? true,
  persistMachineRekey:saved.session.persistMachineRekey.bind(saved.session),
  resolveMachinePairing:saved.session.machinePairing.bind(saved.session),
  allowWrites:options.writes ?? false,nextSequence:async()=>++seq,now:()=>clock.now,
  WebSocket:Socket,BroadcastChannel:null, readTimeoutMs:1000})
 client.events((event: any) => {if(event.type === "error") errors.push(event.error)})
 await client.start({quiet:true})
 await client._receive(JSON.stringify({type:"ready",v:1,role:"viewer",account:authority.account_id,device:authority.viewer_device_id}))
 const frame = await sealEnvelope({ch:"orch/machine-test",seq:10,ts:clock.now,class:"stream",key_id:state.key_id,sender:"machine-test"},
  JSON.stringify({machine:{id:"machine-test",platform:"linux",commands:[]},sessions:[]}),master,saved.machine.privateKey)
 const deliver = (raw: string) => socket.onmessage({data:raw})
 async function drain(until: () => boolean) {
  for(let i=0;i<100 && !until();i++) {await new Promise(resolve=>setTimeout(resolve,2));await client.messageChain}
  assert.ok(until(),"relay processing completed without waiting on its own answer")
 }
 return {...saved,client,viewer,viewerKey,authority,clock,sent,errors,answers,state,master,frame,answer,
  deliver,drain, get socket() {return socket}}
}

test("legitimate read-only B adopts durably via raw relay answer and replays subscriptions without consuming the first envelope", async () => {
 const s = await scenario({auto:true})
 try {
  s.client.subscribe(["orch/machine-test"])
  s.deliver(JSON.stringify({type:"envelope",envelope:s.frame}))
  await s.drain(()=>s.client.machinePairings.get("machine-test")?.epoch===2)
  assert.equal(s.client.allowWrites,false)
  assert.equal((await s.session.machinePairing("machine-test")).keyID,s.state.key_id)
  assert.equal(s.client.sequenceBySender.has("machine-test"),false,"discarded unknown envelope was never delivered")
  assert.equal(s.sent.filter(f=>f.type==="content_key_offer").length,1)
  assert.equal(s.errors[0]?.code,"cloud_reconnecting","refresh uses a transient error, not the pairing repair door")
  assert.deepEqual(s.sent.slice(-2).map(f=>f.type),["unsubscribe","subscribe"])
  s.deliver(JSON.stringify({type:"envelope",envelope:s.frame,realign:true}))
  await s.drain(()=>s.client.sequenceBySender.get("machine-test")===10)
  assert.equal(s.client.viewerRekey.pendingCount,0)
  await assert.rejects(s.client._receive(s.answers[0]))
  s.client.stop()
  await s.client.start({quiet:true})
  await s.client._receive(JSON.stringify({type:"ready",v:1,role:"viewer",account:s.authority.account_id,device:s.authority.viewer_device_id}))
  const afterReconnect=await sealEnvelope({ch:"orch/machine-test",seq:11,ts:s.clock.now,class:"stream",key_id:s.state.key_id,sender:"machine-test"},
   JSON.stringify({machine:{id:"machine-test",platform:"linux",commands:[]},sessions:[]}),s.master,s.machine.privateKey)
  s.deliver(JSON.stringify({type:"envelope",envelope:afterReconnect,realign:true}))
  await s.drain(()=>s.client.sequenceBySender.get("machine-test")===11)
  assert.equal(s.sent.filter(f=>f.type==="content_key_offer").length,1,"reconnect uses the durable adopted key")
 } finally {s.client.stop()}
})

test("default-off, legacy and unpaired machines never initiate rekey from the roster", async () => {
 const s = await scenario({enabled:false})
 try {
  assert.equal(s.client.viewerRekey,null)
  await assert.rejects(s.client._receive(JSON.stringify({type:"envelope",envelope:s.frame})))
  assert.equal(s.sent.filter(f=>f.type==="content_key_offer").length,0)
 } finally {s.client.stop()}
 const t = await scenario()
 try {
  assert.equal(await t.client.viewerRekey.request({...t.pairing,legacy:true}),false)
  t.client.resolveMachinePairing=async()=>null
  await assert.rejects(t.client._receive(JSON.stringify({type:"envelope",envelope:t.frame})))
  assert.equal(t.sent.filter(f=>f.type==="content_key_offer").length,0)
 } finally {t.client.stop()}
})

test("wrong sender/account/machine, unknown answers, tampering and expiry reject; pending is exact and cannot survive restart", async () => {
 const s = await scenario()
 try {
  await assert.rejects(s.client._receive("{\"type\":\"content_key_answer\"}"))
  await assert.rejects(s.client._receive(JSON.stringify({type:"envelope",envelope:{...s.frame,sender:"another"}})))
  assert.equal(s.sent.filter(f=>f.type==="content_key_offer").length,0,"a wrong envelope sender cannot initiate refresh")
  await s.client.viewerRekey.request(s.pairing)
  const offer = rekeyCanonical(s.sent.find(f=>f.type==="content_key_offer")), valid = await s.answer(offer)
  for(const field of ["sender_device_id","signature","offer_hash","machine_ephemeral_key","epoch","key_id"]) {
   const wrong = JSON.parse(valid); wrong[field] = field==="epoch" ? 1 : "wrong"
   await assert.rejects(s.client._receive(rekeyCanonical(wrong)),field)
  }
  for(const field of ["account_id","machine_id","viewer_device_id","operation_id"]) {
   const wrong = JSON.parse(valid); wrong.offer[field]="another"
   await assert.rejects(s.client._receive(rekeyCanonical(wrong)),field)
  }
  s.client.stop()
  await assert.rejects(s.client._receive(valid),"disconnect discards pending")
  const fresh = new ViewerRekey({account:s.authority.account_id,viewer:s.authority.viewer_device_id,
   signing:s.viewer.privateKey,viewerKey:s.viewerKey,now:()=>s.clock.now,send:()=>{},persist:async()=>s.pairing,adopted:()=>{}})
  await assert.rejects(fresh.answer(valid),"restart has no pending operation")
  s.clock.now += VIEWER_REKEY_COOLDOWN_MS
  await s.client.start({quiet:true}); s.client.ready=true
  await s.client.viewerRekey.request(s.pairing)
  const lastOffer = rekeyCanonical(s.sent.filter(f=>f.type==="content_key_offer").at(-1)), expired=await s.answer(lastOffer)
  s.clock.now += VIEWER_REKEY_COOLDOWN_MS
  await assert.rejects(s.client._receive(expired))
 } finally {s.client.stop()}
})

test("storage failure never changes memory; one pending, no retained payload, bounded offers and timeout cleanup", async () => {
 const s = await scenario()
 try {
  await assert.rejects(s.client._receive(JSON.stringify({type:"envelope",envelope:s.frame})))
  for(let i=0;i<20;i++) await assert.rejects(s.client._receive(JSON.stringify({type:"envelope",envelope:s.frame})))
  assert.equal(s.client.viewerRekey.pendingCount,1)
  assert.equal(s.sent.filter(f=>f.type==="content_key_offer").length,1)
  assert.equal(s.client.sequenceBySender.has("machine-test"),false)
  const raw=await s.answer(rekeyCanonical(s.sent.find(f=>f.type==="content_key_offer")))
  s.db.failNextCommit()
  await assert.rejects(s.client._receive(raw))
  assert.equal(s.client.machinePairings.get("machine-test").keyID,"ms-1")
  assert.equal((await s.session.machinePairing("machine-test")).keyID,"ms-1")
  await s.client._receive(raw)
  assert.equal(s.client.machinePairings.get("machine-test").epoch,2)
  assert.equal(await s.client.viewerRekey.request(s.pairing),false,"cooldown survives consume")
  s.clock.now += VIEWER_REKEY_COOLDOWN_MS
  assert.equal(await s.client.viewerRekey.request({...s.pairing,machineID:"another"}),true)
  s.client.stop()
  assert.equal(s.client.viewerRekey.pendingCount,0)
 } finally {s.client.stop()}
})

test("A's old group key alone provides neither a signing pin nor the ephemeral needed to recover B's new key", async () => {
 const s = await scenario()
 try {
  await s.client.viewerRekey.request(s.pairing)
  const offer = rekeyCanonical(s.sent.find(f=>f.type==="content_key_offer")), raw=await s.answer(offer)
  const attacker = await crypto.subtle.generateKey("X25519",true,["deriveBits"]) as CryptoKeyPair
  const pin = new Uint8Array(await crypto.subtle.exportKey("raw",s.machine.publicKey))
  await assert.rejects(openRekeyAnswer(raw,offer,s.authority,attacker.privateKey,pin,1,"ms-1",s.clock.now))
  const impostor=await crypto.subtle.generateKey("Ed25519",false,["sign","verify"])
  const impostorOffer={...JSON.parse(offer),signature:""}
  // A knows ms-1, but a machine verifies the active viewer signing pin, never that key.
  // @ts-expect-error Node executes TypeScript directly.
  const {signRekeyOffer}=await import("./rekey-crypto.ts")
  const signed=await signRekeyOffer(impostorOffer,impostor.privateKey,s.clock.now)
  await assert.rejects(s.answer(signed))
  assert.notEqual(s.state.key_id,"ms-1")
 } finally {s.client.stop()}
})

test("expiry timer releases the single ephemeral slot without retrying; floods and other machines cannot add operations", async () => {
 const s=await scenario()
 let expire: (()=>void) | undefined, cleared=0
 const offers: string[]=[]
 const rekey=new ViewerRekey({account:s.authority.account_id,viewer:s.authority.viewer_device_id,
  signing:s.viewer.privateKey,viewerKey:s.viewerKey,now:()=>s.clock.now,send:(raw: string)=>offers.push(raw),
  persist:async()=>s.pairing,adopted:()=>{},
  setTimeout:((run: ()=>void)=>{expire=run;return 1}) as any,clearTimeout:(()=>{cleared++}) as any})
 try {
  const first=rekey.request(s.pairing)
  assert.equal(await rekey.request({...s.pairing,machineID:"another"}),false)
  await first
  assert.equal(rekey.pendingCount,1); assert.equal(offers.length,1)
  s.clock.now+=VIEWER_REKEY_COOLDOWN_MS; expire!()
  assert.equal(rekey.pendingCount,0); assert.equal(offers.length,1); assert.equal(cleared,1)
  await assert.rejects(rekey.answer(await s.answer(offers[0]).catch(()=>"{}")))
 } finally {rekey.cancel();s.client.stop()}
})

test("real durable mk epoch/hash IDs seal and open; 128 bytes fit and 129 bytes are rejected", async () => {
 const s = await scenario()
 try {
  assert.ok(s.state.key_id.length > 64)
  const pin=await importSenderPublicKey(new Uint8Array(await crypto.subtle.exportKey("raw",s.machine.publicKey)))
  assert.deepEqual(JSON.parse(new TextDecoder().decode(await openEnvelope(s.frame,s.master,pin))).sessions,[])
  for(const size of [128,129]) {
   const fields={ch:"orch/machine-test",seq:1,ts:s.clock.now,class:"stream",key_id:"k".repeat(size),sender:"machine-test"}
   if(size===129) await assert.rejects(sealEnvelope(fields,"{}",s.master,s.machine.privateKey),/bad key_id/)
   else assert.equal((await sealEnvelope(fields,"{}",s.master,s.machine.privateKey)).key_id.length,128)
  }
 } finally {s.client.stop()}
})

test("queued idempotent reads resume after adoption while other machine bindings and account key remain unchanged", async () => {
 const s = await scenario({writes:true})
 try {
  const other={...s.pairing,machineID:"other",senderID:"other"}
  s.client.machinePairings.set("other",other)
  s.client._sessionIdentity = () => ({machine:"machine-test",session:"session-test"})
  s.client._unsupportedRefusal = () => null
  const reading=s.client._read({machine:"machine-test",session:"session-test"},"git",{},"git")
  await s.drain(()=>s.sent.some(f=>f.type==="publish"))
  await assert.rejects(s.client._receive(JSON.stringify({type:"envelope",envelope:s.frame})))
  const before=s.sent.filter(f=>f.type==="publish").length
  const raw=await s.answer(rekeyCanonical(s.sent.find(f=>f.type==="content_key_offer")))
  await s.client._receive(raw)
  await s.drain(()=>s.sent.filter(f=>f.type==="publish").length===before+1)
  const resent=s.sent.filter(f=>f.type==="publish").at(-1)
  assert.equal(resent.envelope.key_id,s.state.key_id)
  const viewerPin=await importSenderPublicKey(s.viewerKey)
  const clear=await openEnvelope(resent.envelope,s.master,viewerPin)
  assert.equal(JSON.parse(new TextDecoder().decode(clear)).type,"git")
  const reply=await sealEnvelope({ch:"t/machine-test/session-test",seq:11,ts:s.clock.now,class:"stream",key_id:s.state.key_id,sender:"machine-test"},
   JSON.stringify({read:"git",body:{branch:"main"}}),s.master,s.machine.privateKey)
  await s.client._receive(JSON.stringify({type:"envelope",envelope:reply}))
  assert.deepEqual(await reading,{branch:"main"})
  assert.equal(s.client.machinePairings.get("other"),other)
  assert.equal(s.db.table.get(s.session.masterKeyIDName()),"ms-1")
 } finally {s.client.stop()}
})
