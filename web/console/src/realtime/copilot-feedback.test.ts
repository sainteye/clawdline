import {test} from 'node:test'
import assert from 'node:assert/strict'
// @ts-expect-error -- Node executes the TypeScript source.
import {callWorkEvidence,callProgress,deliveryQuestion,ProgressCue} from './copilot-feedback.ts'
const input={id:'request',call:'call',text:'Send this requirement to the Session.',planned:false,at:1}
const state={id:'workspace',revision:1,plan_version:0,inputs:[input],tasks:[]}
test('saving or planning a request does not establish message delivery',()=>{
 const result=callWorkEvidence({...state,inputs:[{...input,planning_error:'voice_work_timeout',planning_stage:'provider'}]},'call',false)
 assert.equal(result.messages.length,0)
 assert.equal(result.delivery_confirmed,false)
 assert.equal(result.requests[0].error,'voice_work_timeout')
})
test('only a successful send receipt establishes delivery; manual completion does not',()=>{
 const task={id:'send',tool:'send_message_to_session',text:'Send requirement',args:{session_id:'one'},sources:['request'],depends_on:[],priority:0,version:1,status:'waiting_confirmation'}
 for(const [status,result,received] of ([['waiting_confirmation','',false],['queued','',false],['outcome_unknown','',false],['done','Completed by the person.',false],['waiting_result','{"success":true,"sent_to":"one"}',false],['waiting_result','{"success":true,"sent_to":"one","receipt":{"action":"typed"}}',true]] as [string,string,boolean][])){
  const evidence=callWorkEvidence({...state,tasks:[{...task,status,result}]},'call',false)
  assert.equal(evidence.delivery_confirmed,received)
 }
})
test('status questions request fresh work evidence instead of inferring from Session silence',()=>{
 for(const text of ['你有送出嗎','我看他沒有收到你說的訊息啊','Did you send it?','Has it been delivered?'])assert.equal(deliveryQuestion(text),true,text)
 assert.equal(deliveryQuestion('Please summarize this Session.'),false)
})
test('waiting confirmation and failed planning are stopped states, not active work',()=>{
 assert.equal(callProgress({saving:false,reading:false,responding:false,work:callWorkEvidence({...state,inputs:[{...input,planning_error:'voice_work_timeout'}]},'call',false)})?.phase,'failed')
 assert.equal(callProgress({saving:false,reading:true,responding:false})?.phase,'reading')
})
test('processing cue sounds once per utterance and never over either speaker',()=>{
 let played=0,now=0;const cue=new ProgressCue(()=>played++,()=>now)
 cue.update(true,true);assert.equal(played,0,'never over a speaker')
 cue.newTurn();cue.update(true,false);cue.update(true,false);assert.equal(played,1,'once when work starts')
 now=1000;cue.update(true,false,1);assert.equal(played,2,'again when a lookup starts')
 now=3000;cue.update(true,false,1);assert.equal(played,2,'not in a burst')
 now=5000;cue.update(true,false,1);assert.equal(played,3,'again while a wait stays silent')
 now=9500;cue.update(true,true,1);assert.equal(played,3,'not while the assistant speaks')
 cue.update(false,false);now=20000;cue.update(false,false);assert.equal(played,3,'silent when idle')
})
