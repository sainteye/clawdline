// Inactive v1 control contract. Caller owns active pins and durable replay state.
export const REKEY_LIFETIME_MS_LIMIT = 120_000
export const MAX_REKEY_BODY_BYTES = 4096
export interface RekeyOffer {
 type: string; v: number; account_id: string; machine_id: string; viewer_device_id: string;
 operation_id: string; from_epoch: number; from_key_id: string; viewer_ephemeral_key: string;
 nonce: string; expires_at_ms: number; signature: string;
}
export interface RekeyAnswer {
 type: string; v: number; offer: RekeyOffer; offer_hash: string; sender_device_id: string;
 machine_ephemeral_key: string; epoch: number; key_id: string; sealing_nonce: string;
 ciphertext: string; signature: string;
}
export interface RekeyAuthority { account_id: string; machine_id: string; viewer_device_id: string; activeViewerKey: Uint8Array }
const enc = new TextEncoder()
const fail = (): never => { throw new Error("content key exchange rejected") }
export function rekeyCanonical(v: unknown): string {
 if (v !== null && typeof v === "object" && !Array.isArray(v)) return "{" + Object.entries(v).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([k,x]) => JSON.stringify(k)+":"+rekeyCanonical(x)).join(",") + "}"
 return JSON.stringify(v)
}
const omit = (v: object, ...names: string[]) => Object.fromEntries(Object.entries(v).filter(([k]) => !names.includes(k)))
const bytes = (domain: string, v: unknown) => enc.encode("clawdline-content-key-"+domain+"-v1\0"+rekeyCanonical(v))
const b64 = (v: Uint8Array) => btoa(String.fromCharCode(...v))
const unb64 = (s: string,n: number): Uint8Array<ArrayBuffer> => {
 try {const v=Uint8Array.from(atob(s), c=>c.charCodeAt(0)); if(v.length!==n || b64(v)!==s) fail(); return v} catch {return fail()}
}
const raw = (v: Uint8Array) => new Uint8Array(v).buffer
const fields = (v: object, names: string) => { if(Object.keys(v).sort().join(" ")!==names.split(" ").sort().join(" ")) fail() }
const offerFields="type v account_id machine_id viewer_device_id operation_id from_epoch from_key_id viewer_ephemeral_key nonce expires_at_ms signature"
const answerFields="type v offer offer_hash sender_device_id machine_ephemeral_key epoch key_id sealing_nonce ciphertext signature"
const parse = <T>(s: string): T => { if(enc.encode(s).length>MAX_REKEY_BODY_BYTES) fail(); const v=JSON.parse(s); if(!v || typeof v!=="object" || Array.isArray(v) || rekeyCanonical(v)!==s) fail(); return v as T }
const id = (s: string) => typeof s==="string" && /^[\x21-\x7e]{1,128}$/.test(s) && !/[<>&]/.test(s)
const epoch = (n: number) => Number.isSafeInteger(n) && n>0
function validOffer(o: RekeyOffer, now: number) {
 fields(o,offerFields)
 if(o.type!=="content_key_offer" || o.v!==1 || !epoch(o.from_epoch) || !Number.isSafeInteger(now) || now<0 || !Number.isSafeInteger(o.expires_at_ms) || o.expires_at_ms<=now || o.expires_at_ms-now>REKEY_LIFETIME_MS_LIMIT || ![o.account_id,o.machine_id,o.viewer_device_id,o.operation_id,o.from_key_id].every(id)) fail()
 unb64(o.viewer_ephemeral_key,32); unb64(o.nonce,32)
}
async function verify(key: Uint8Array | CryptoKey, sig: string, data: Uint8Array) {
 if(key instanceof Uint8Array && key.length!==32) fail()
 const k=key instanceof Uint8Array ? await crypto.subtle.importKey("raw",raw(key),"Ed25519",false,["verify"]) : key
 if(!await crypto.subtle.verify("Ed25519",k,raw(unb64(sig,64)),raw(data))) fail()
}
export async function signRekeyOffer(o: RekeyOffer, signing: CryptoKey, now: number): Promise<string> {
 validOffer(o,now); return rekeyCanonical({...o,signature:b64(new Uint8Array(await crypto.subtle.sign("Ed25519",signing,raw(bytes("offer",omit(o,"signature"))))))})
}
export async function verifyRekeyOffer(s: string,a: RekeyAuthority,now: number): Promise<RekeyOffer> {
 const o=parse<RekeyOffer>(s); validOffer(o,now)
 if(o.account_id!==a.account_id || o.machine_id!==a.machine_id || o.viewer_device_id!==a.viewer_device_id) fail()
 await verify(a.activeViewerKey,o.signature,bytes("offer",omit(o,"signature"))); return o
}
async function publicEphemeral(k: CryptoKey): Promise<string> {
 const j=await crypto.subtle.exportKey("jwk",k); if(!j.x || j.crv!=="X25519" || !j.d) fail()
 return b64(Uint8Array.from(atob(j.x!.replace(/-/g,"+").replace(/_/g,"/")),c=>c.charCodeAt(0)))
}
async function shared(k: CryptoKey,peer: string) {
 const p=await crypto.subtle.importKey("raw",raw(unb64(peer,32)),"X25519",false,[])
 return crypto.subtle.deriveBits({name:"X25519",public:p},k,256)
}
async function aes(k: CryptoKey,peer: string,a: RekeyAnswer) {
 const secret=await shared(k,peer)
 const hk=await crypto.subtle.importKey("raw",secret,"HKDF",false,["deriveKey"])
 return crypto.subtle.deriveKey({name:"HKDF",hash:"SHA-256",salt:raw(unb64(a.offer_hash,32)),info:raw(bytes("kdf",omit(a,"signature","ciphertext")))},hk,{name:"AES-GCM",length:256},false,["encrypt","decrypt"])
}
async function hash(s: string) {return b64(new Uint8Array(await crypto.subtle.digest("SHA-256",raw(enc.encode(s)))))}
export async function rekeyKeyID(e: number,k: Uint8Array) {
 if(!epoch(e) || k.length!==32) fail()
 if(e===1) return "ms-1"
 return "mk-"+e+"-"+Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256",raw(k))),x=>x.toString(16).padStart(2,"0")).join("")
}
// Fresh extractable X25519 ephemeral and random 12-byte nonce per answer.
export async function sealRekeyAnswer(s: string,a: RekeyAuthority,state: {epoch:number;key_id:string;key:Uint8Array},signing: CryptoKey,ephemeral: CryptoKey,nonce: Uint8Array,now: number): Promise<string> {
 const o=await verifyRekeyOffer(s,a,now)
 if(!epoch(state.epoch) || state.epoch<o.from_epoch || state.key_id!==await rekeyKeyID(state.epoch,state.key) || (state.epoch===o.from_epoch && state.key_id!==o.from_key_id) || nonce.length!==12) fail()
 const ans: RekeyAnswer={type:"content_key_answer",v:1,offer:o,offer_hash:await hash(s),sender_device_id:a.machine_id,machine_ephemeral_key:await publicEphemeral(ephemeral),epoch:state.epoch,key_id:state.key_id,sealing_nonce:b64(nonce),ciphertext:"",signature:""}
 const k=await aes(ephemeral,o.viewer_ephemeral_key,ans)
 ans.ciphertext=b64(new Uint8Array(await crypto.subtle.encrypt({name:"AES-GCM",iv:raw(nonce),additionalData:raw(bytes("aad",omit(ans,"signature","ciphertext"))),tagLength:128},k,raw(state.key))))
 ans.signature=b64(new Uint8Array(await crypto.subtle.sign("Ed25519",signing,raw(bytes("answer",omit(ans,"signature"))))))
 return rekeyCanonical(ans)
}
// Consume pending + persist state atomically in runtime. This has no replay cache.
export async function openRekeyAnswer(s: string,pending: string,a: RekeyAuthority,privateKey: CryptoKey,machineKey: Uint8Array | CryptoKey,storedEpoch: number,storedKeyID: string,now: number) {
 const o=await verifyRekeyOffer(pending,a,now), ans=parse<RekeyAnswer>(s); fields(ans,answerFields)
 if(ans.type!=="content_key_answer" || ans.v!==1 || rekeyCanonical(ans.offer)!==pending || ans.offer_hash!==await hash(pending) || ans.sender_device_id!==a.machine_id || !epoch(ans.epoch) || ans.epoch<o.from_epoch || ans.epoch<storedEpoch || (ans.epoch===storedEpoch && ans.key_id!==storedKeyID) || (ans.epoch===o.from_epoch && ans.key_id!==o.from_key_id)) fail()
 await verify(machineKey,ans.signature,bytes("answer",omit(ans,"signature")))
 if(await publicEphemeral(privateKey)!==o.viewer_ephemeral_key) fail()
 const k=await aes(privateKey,ans.machine_ephemeral_key,ans)
 const key=new Uint8Array(await crypto.subtle.decrypt({name:"AES-GCM",iv:raw(unb64(ans.sealing_nonce,12)),additionalData:raw(bytes("aad",omit(ans,"signature","ciphertext"))),tagLength:128},k,raw(unb64(ans.ciphertext,48))))
 if(await rekeyKeyID(ans.epoch,key)!==ans.key_id) fail()
 return {epoch:ans.epoch,key_id:ans.key_id,key}
}
