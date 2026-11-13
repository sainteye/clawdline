import type {BatchOffer,VoiceState} from './engine.js'
export interface BatchProposal {workspace:string;offer:BatchOffer;titles:string[];announced:boolean}
export function batchKey(offer?:BatchOffer|null){return offer?JSON.stringify([offer.epoch,offer.limit,offer.tasks.map(t=>[t.id,t.version])]):''}
export function batchProposal(state:VoiceState|null,offer?:BatchOffer|null):BatchProposal|undefined {
 if(!state||!offer)return undefined
 const titles=offer.tasks.map(ref=>state.tasks.find(t=>t.id===ref.id&&t.version===ref.version)?.text)
 if(!titles.length||titles.some(t=>!t))return undefined
 return {workspace:state.id,offer:structuredClone(offer),titles:titles as string[],announced:false}
}
function normalized(text:string){return text.normalize('NFKC').toLocaleLowerCase().replace(/[\p{P}\p{Z}\s]/gu,'')}
export function continuationConsent(text:string){return ['可以繼續','繼續下一批','確認下一批','可以继续','继续下一批','yescontinue','continuethenextbatch'].includes(normalized(text))}
export function batchSpoken(text:string,proposal:BatchProposal){const spoken=normalized(text);return proposal.titles.every(title=>spoken.includes(normalized(title)))}
export const BATCH_GUIDANCE='The automatic continuation allowance has been used. These are the exact next candidate tasks, not executed yet. Read their titles verbatim, in order, then ask whether to continue this batch. Say the person can say “可以，繼續” or “yes, continue”. Previous failures or cancellations are not successes. Do not claim these tasks are running. This approval is only for these read tasks; important actions, ambiguity and missing facts still require separate decisions. Never use a model confidence score as authorization.'
