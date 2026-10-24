import { test } from "node:test"
import assert from "node:assert/strict"
import { adoptMachineRekey, importMasterSecret, importSenderPublicKey, loadMachineCryptoState, storePairingCryptoKeys, storePairingBinding } from "../legacy/js/net/cloud-crypto.js"
import { CloudClient } from "../legacy/js/net/cloud-client.js"
// @ts-expect-error Node executes TypeScript directly.
import {rekeyKeyID,sealRekeyAnswer} from "./rekey-crypto.ts"
import { CloudViewerSession } from "../legacy/js/net/cloud-boot.js"

/** Same serialized readwrite model as viewer-sequence.test.ts, with transaction-local writes
 * and abort rollback. Only this synthetic database is touched; no browser profile is read. */
export function rekeyDatabase() {
 const table = new Map<string, unknown>()
 let queue = Promise.resolve(), failCommit = false
 const factory = { open() {
  const request: any = {}
  setImmediate(() => {
   request.result = {
    objectStoreNames: { contains: () => true }, close() {},
    transaction() {
     const tx: any = {}, steps: (() => void)[] = []
     let local: Map<string, unknown>, aborted = false
     tx.abort = () => { aborted = true }
     tx.objectStore = () => ({
      get(key: string) { const r: any = {}; steps.push(() => { r.result = structuredClone(local.get(key)); r.onsuccess?.() }); return r },
      put(value: unknown, key: string) { steps.push(() => { local.set(key, structuredClone(value)) }) },
     })
     queue = queue.then(() => new Promise<void>(done => {
      local = new Map(table)
      const drain = () => setImmediate(() => {
       if (aborted || (!steps.length && failCommit)) {
        failCommit = false; tx.error = new Error("synthetic transaction failure"); tx.onabort?.(); done()
       } else if (steps.length) { steps.shift()!(); drain() }
       else { table.clear(); for (const [k,v] of local) table.set(k,v); tx.oncomplete?.(); done() }
      })
      drain()
     }))
     return tx
    },
   }
   request.onsuccess?.()
  })
  return request
 } }
 return { factory: factory as unknown as IDBFactory, table, failNextCommit() { failCommit = true } }
}

export async function storedPairing() {
 const db = rekeyDatabase()
 const machine = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"])
 const senderKey = await importSenderPublicKey(new Uint8Array(await crypto.subtle.exportKey("raw", machine.publicKey)))
 const masterKey = await importMasterSecret(new Uint8Array(32).fill(1))
 const session = new CloudViewerSession({ config: {apiOrigin:"https://api.example.test",appOrigin:"https://app.example.test"}, indexedDB: db.factory }) as any
 session.account = "account-test"
 const names = {bindingName:session.machineBindingName("machine-test"), masterName:session.machineMasterKeyName("machine-test"),
  masterKeyIDName:session.machineMasterKeyIDName("machine-test"), senderName:session.machineSenderKeyName("machine-test","machine-test")}
 await storePairingCryptoKeys({...names, masterKey, senderKey, keyID:"ms-1",
  binding:{v:1,machineID:"machine-test",senderID:"machine-test",keyID:"ms-1",legacy:false},
  preserve: {masterName:session.masterKeyName(), masterKeyIDName:session.masterKeyIDName(),senderName:session.senderKeyName("machine-test")}}, db.factory)
 return {db, machine, session, names, pairing:await session.machinePairing("machine-test")}
}

test("two tabs serialize floor comparisons; restart reads the complete newest tuple and preserves account aliases", async () => {
 const {db,session,names,pairing} = await storedPairing()
 const newer = {epoch:3,key_id:"mk-3-"+"a".repeat(64),key:new Uint8Array(32).fill(3)}
 const older = {epoch:2,key_id:"mk-2-"+"b".repeat(64),key:new Uint8Array(32).fill(2)}
 const results = await Promise.allSettled([
  session.persistMachineRekey(pairing,newer,"newer",()=>true),
  session.persistMachineRekey(pairing,older,"older",()=>true),
 ])
 assert.equal(results[0].status,"fulfilled"); assert.equal(results[1].status,"rejected")
 const snapshot: any = await loadMachineCryptoState(names,db.factory)
 assert.equal(snapshot.bindingName.epoch,3); assert.equal(snapshot.masterKeyIDName,newer.key_id)
 assert.equal((await session.machinePairing("machine-test")).epoch,3)
 assert.equal(db.table.get(session.masterKeyIDName()),"ms-1")
 await assert.rejects(session.persistMachineRekey(pairing,newer,"newer",()=>true))
 const equalButDifferent = {...newer,key_id:"mk-3-"+"c".repeat(64)}
 await assert.rejects(session.persistMachineRekey(pairing,equalButDifferent,"different",()=>true))
 await assert.rejects(storePairingBinding(names.bindingName,
  {v:1,machineID:"machine-test",senderID:"machine-test",keyID:"ms-1",legacy:true},db.factory))
 await assert.rejects(storePairingCryptoKeys({...names,masterKey:pairing.masterKey,senderKey:pairing.senderKey,keyID:"ms-1",
  binding:{v:1,machineID:"machine-test",senderID:"machine-test",keyID:"ms-1",epoch:1,legacy:false}},db.factory))
 assert.equal((await session.machinePairing("machine-test")).epoch,3)
})

test("aborted storage and canceled pending keep every durable field unchanged; explicit re-pair invalidates old pending", async () => {
 const {db,session,names,pairing} = await storedPairing()
 const state = {epoch:2,key_id:"mk-2-"+"a".repeat(64),key:new Uint8Array(32).fill(2)}
 db.failNextCommit()
 await assert.rejects(session.persistMachineRekey(pairing,state,"failure",()=>true))
 assert.equal((await session.machinePairing("machine-test")).keyID,"ms-1")
 await assert.rejects(session.persistMachineRekey(pairing,state,"canceled",()=>false))
 await storePairingCryptoKeys({...names,masterKey:pairing.masterKey,senderKey:pairing.senderKey,keyID:"ms-1",
  binding:{v:1,machineID:"machine-test",senderID:"machine-test",keyID:"ms-1",legacy:false}},db.factory)
 await assert.rejects(session.persistMachineRekey(pairing,state,"old-generation",()=>true))
 assert.equal((await session.machinePairing("machine-test")).epoch,1)
 assert.equal(db.table.get(names.masterKeyIDName),"ms-1")
})

test("legacy bindings and invalid floors cannot be adopted", async () => {
 const {db,names,pairing} = await storedPairing()
 db.table.set(names.bindingName,{v:1,machineID:"machine-test",senderID:"machine-test",legacy:true,keyID:"ms-1"})
 await assert.rejects(adoptMachineRekey({...names,expected:pairing,masterKey:pairing.masterKey,
  epoch:2,keyID:"next",operation:"test",valid:()=>true},db.factory))
})


async function recoveryClient(s: Awaited<ReturnType<typeof storedPairing>>) {
 const viewer = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"]) as CryptoKeyPair
 const publicKey = new Uint8Array(await crypto.subtle.exportKey("raw", viewer.publicKey))
 const clock = {now:1700000000000}, sent: string[] = []
 const client: any = new CloudClient({relayURL:"wss://relay.example.test",deviceToken:"synthetic",
  account:"account-test",deviceID:"viewer-test",devicePrivateKey:viewer.privateKey,devicePublicKey:publicKey,
  enableViewerRekey:true,now:()=>clock.now,BroadcastChannel:null,
  persistMachineRekey:s.session.persistMachineRekey.bind(s.session),resolveMachinePairing:s.session.machinePairing.bind(s.session)})
 client.ready=true;client.socket={readyState:1,send:(raw:string)=>sent.push(raw),close(){}}
 async function answer() {
  const ephemeral=await crypto.subtle.generateKey("X25519",true,["deriveBits"]) as CryptoKeyPair
  const key=crypto.getRandomValues(new Uint8Array(32))
  return sealRekeyAnswer(sent.at(-1)!,{account_id:"account-test",machine_id:"machine-test",viewer_device_id:"viewer-test",activeViewerKey:publicKey},
   {epoch:2,key_id:await rekeyKeyID(2,key),key},s.machine.privateKey,ephemeral.privateKey,crypto.getRandomValues(new Uint8Array(12)),clock.now)
 }
 async function repair() {
  await storePairingCryptoKeys({...s.names,masterKey:s.pairing.masterKey,senderKey:s.pairing.senderKey,keyID:"ms-1",
   binding:{v:1,machineID:"machine-test",senderID:"machine-test",keyID:"ms-1",legacy:false}},s.db.factory)
  return s.session.machinePairing("machine-test")
 }
 return {client,clock,answer,repair}
}

test("an existing client reloads a repaired positive pairing and completes the next rekey",async()=>{
 const s=await storedPairing(), r=await recoveryClient(s), c=r.client
 try {
  const old=await c._machinePairing("machine-test")
  const repaired=await r.repair()
  assert.notEqual(old.revision,repaired.revision)
  c.forgetMachinePairingAnswer("machine-test")
  assert.equal((await c._machinePairing("machine-test")).revision,repaired.revision)
  assert.equal(await c.viewerRekey.request(await c._machinePairing("machine-test")),true)
  assert.equal(await c._receive(await r.answer()),true)
  assert.equal((await s.session.machinePairing("machine-test")).epoch,2)
 } finally {c.stop()}
})

test("a paired notification cancels the old machine operation and rejects an in-flight old lookup",async()=>{
 const s=await storedPairing(),r=await recoveryClient(s),c=r.client
 try {
  const old=await c._machinePairing("machine-test")
  assert.equal(await c.viewerRekey.request(old),true)
  c._forgetPairingAnswer("machine-test")
  assert.equal(c.viewerRekey.pendingCount,0)
  let release!:(v:unknown)=>void
  c.resolveMachinePairing=()=>new Promise(resolve=>{release=resolve})
  const pending=c._machinePairing("machine-test")
  c._forgetPairingAnswer("machine-test")
  release(old)
  await assert.rejects(pending,{code:"cloud_reconnecting"})
  const repaired=await r.repair()
  c.resolveMachinePairing=s.session.machinePairing.bind(s.session)
  assert.equal((await c._machinePairing("machine-test")).revision,repaired.revision)
 } finally {c.stop()}
})

test("a missed pairing notification is repaired after the durable revision conflict",async()=>{
 const s=await storedPairing(),r=await recoveryClient(s),c=r.client
 try {
  const old=await c._machinePairing("machine-test")
  const repaired=await r.repair()
  assert.equal(await c.viewerRekey.request(old),true)
  await assert.rejects(c._receive(await r.answer()),{code:"cloud_reconnecting"})
  assert.equal(c.viewerRekey.pendingCount,0)
  assert.equal((await c._machinePairing("machine-test")).revision,repaired.revision)
  r.clock.now+=120000
  assert.equal(await c.viewerRekey.request(await c._machinePairing("machine-test")),true)
  assert.equal(await c._receive(await r.answer()),true)
  assert.equal((await s.session.machinePairing("machine-test")).epoch,2)
 } finally {c.stop()}
})


test("v2 strong storage without epoch or revision durably rekeys and survives a fresh session",async()=>{
 const s=await storedPairing()
 // v2.0.0 storePairingCryptoKeys persisted this synthetic binding shape;
 // its nonextractable machine AES and Ed25519 CryptoKeys occupy the same names.
 s.db.table.set(s.names.bindingName,{v:1,machineID:"machine-test",senderID:"machine-test",keyID:"ms-1",legacy:false})
 s.pairing=await s.session.machinePairing("machine-test")
 assert.equal(s.pairing.epoch,1);assert.equal(s.pairing.revision,undefined)
 const r=await recoveryClient(s)
 try {
  assert.equal(await r.client.viewerRekey.request(s.pairing),true)
  assert.equal(await r.client._receive(await r.answer()),true)
  const reboot:any=new CloudViewerSession({config:{apiOrigin:"https://api.example.test",appOrigin:"https://app.example.test"},indexedDB:s.db.factory})
  reboot.account="account-test"
  const restored=await reboot.machinePairing("machine-test")
  assert.equal(restored.epoch,2)
  const clear=new TextEncoder().encode("future-canary"),iv=new Uint8Array(12)
  const ciphertext=await crypto.subtle.encrypt({name:"AES-GCM",iv},restored.masterKey,clear)
  assert.deepEqual(new Uint8Array(await crypto.subtle.decrypt({name:"AES-GCM",iv},restored.masterKey,ciphertext)),clear)
  await assert.rejects(crypto.subtle.decrypt({name:"AES-GCM",iv},s.pairing.masterKey,ciphertext))
  s.db.table.set(s.names.bindingName,{v:1,machineID:"machine-test",senderID:"machine-test",keyID:"ms-1",legacy:true})
  const legacy=await reboot.machinePairing("machine-test")
  assert.equal(legacy.legacy,true)
  r.clock.now+=120000
  assert.equal(await r.client.viewerRekey.request(legacy),false)
 }finally{r.client.stop()}
})
