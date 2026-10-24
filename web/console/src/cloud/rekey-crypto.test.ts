import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync,writeFileSync} from 'node:fs'
// @ts-expect-error Node executes TypeScript directly.
import {signRekeyOffer,verifyRekeyOffer,sealRekeyAnswer,openRekeyAnswer,rekeyCanonical,rekeyKeyID} from './rekey-crypto.ts'
const b=(s:string)=>new Uint8Array(Buffer.from(s,'base64'))
const enc=(n:number)=>Buffer.alloc(32,n).toString('base64')
async function privateKey(kind:string,s:string) {
 const prefix=kind==='Ed25519'?'302e020100300506032b657004220420':'302e020100300506032b656e04220420'
 return crypto.subtle.importKey('pkcs8',Buffer.concat([Buffer.from(prefix,'hex'),Buffer.from(s,'base64')]),kind,true,kind==='Ed25519'?['sign']:['deriveBits'])
}
async function pub(k:CryptoKey) {const j=await crypto.subtle.exportKey('jwk',k);return new Uint8Array(Buffer.from(j.x!,'base64url'))}
const fixtureURL=new URL('../../../../internal/domain/cloud/testdata/rekey-v1.json',import.meta.url)
async function setup() {
 const f=process.env.REKEY_GENERATE?{viewer_seed:enc(1),machine_seed:enc(2),viewer_private:enc(3),machine_private:enc(4),content_key:enc(5),sealing_nonce:Buffer.alloc(12,6).toString('base64'),offer:'',answer:''}:JSON.parse(readFileSync(fixtureURL,'utf8'))
 const v=await privateKey('Ed25519',f.viewer_seed),m=await privateKey('Ed25519',f.machine_seed),vp=await privateKey('X25519',f.viewer_private),mp=await privateKey('X25519',f.machine_private)
 const a={account_id:'account-test',machine_id:'machine-test',viewer_device_id:'viewer-test',activeViewerKey:await pub(v)}
 return {f,v,m,vp,mp,a}
}
const now=1700000000000
test('independent WebCrypto produces and opens Go golden bytes',async()=>{
 const {f,v,m,vp,mp,a}=await setup()
 const offer=await signRekeyOffer({type:'content_key_offer',v:1,account_id:a.account_id,machine_id:a.machine_id,viewer_device_id:a.viewer_device_id,operation_id:'operation-test',from_epoch:1,from_key_id:'ms-1',viewer_ephemeral_key:Buffer.from(await pub(vp)).toString('base64'),nonce:enc(7),expires_at_ms:now+120000,signature:''},v,now)
 const answer=await sealRekeyAnswer(offer,a,{epoch:2,key_id:await rekeyKeyID(2,b(f.content_key)),key:b(f.content_key)},m,mp,b(f.sealing_nonce),now)
 if(process.env.REKEY_GENERATE) {writeFileSync(fixtureURL,JSON.stringify({...f,offer,answer},null,2)+'\n');return}
 assert.equal(offer,f.offer);assert.equal(answer,f.answer)
 assert.deepEqual((await openRekeyAnswer(f.answer,f.offer,a,vp,await pub(m),1,'ms-1',now)).key,b(f.content_key))
})
test('tampering, inactive pin, wrong recipient, replay floor and malformed input reject',async()=>{
 const {f,m,vp,mp,a}=await setup()
 for(const field of Object.keys(JSON.parse(f.offer))) {const x=JSON.parse(f.offer);x[field]=typeof x[field]==='number'?x[field]+1:'tampered';await assert.rejects(verifyRekeyOffer(rekeyCanonical(x),a,now),field)}
 for(const field of [...Object.keys(JSON.parse(f.answer)),'extra']) {const x=JSON.parse(f.answer);x[field]=typeof x[field]==='number'?x[field]+1:'tampered';await assert.rejects(openRekeyAnswer(rekeyCanonical(x),f.offer,a,vp,await pub(m),1,'ms-1',now),field)}
 await assert.rejects(verifyRekeyOffer(f.offer,{...a,activeViewerKey:new Uint8Array()},now))
 await assert.rejects(openRekeyAnswer(f.answer,f.offer,a,mp,await pub(m),1,'ms-1',now))
 await assert.rejects(openRekeyAnswer(f.answer,f.offer,a,vp,await pub(m),3,'newer',now))
 await assert.rejects(verifyRekeyOffer(f.offer,a,now+120000))
 for(const s of ['{}',f.offer+' ','x'.repeat(4097)]) await assert.rejects(verifyRekeyOffer(s,a,now))
})
