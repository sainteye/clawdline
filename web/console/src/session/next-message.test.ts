import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strip-types uses source extensions.
import { PendingSends, turnWords } from "./pending.ts"
// @ts-expect-error -- Node strip-types uses source extensions.
import { Cards, KEPT_CARDS } from "./persist.ts"
// @ts-expect-error -- Node strip-types uses source extensions.
import { NextMessages } from "./next-message.ts"

test("busy Gemini inputs queue, survive reload and send once after an allowed idle observation", () => {
 const cards = new PendingSends(), queue = new NextMessages(cards)
 const first = queue.add("owned:acp:one", "same words", [], 100000)!
 const second = queue.add("owned:acp:one", "same words", [], 100001)!
 cards.reconcile(first.session, [{role:"user",text:first.text,at:101}],102000)
 assert.equal(cards.of(first.session).length,2,"unsent queued input is not a delivered user echo")
 assert.equal(queue.take(first.session,false,"obs1",true,102000),null)
 assert.equal(queue.take(first.session,true,"obs1",false,102000),null)
 assert.equal(queue.take("other",true,"obs1",true,102000),null)
 let saved="";const store=new Cards({store:{read:()=>saved,write:s=>{saved=s}},turnWords})
 store.restore("",102000)
 store.keep(cards.all(),102000)
 const restored=new PendingSends();restored.restore(store.restore("",102001))
 assert.equal(restored.card(first.token)?.state,"queued")
 const resumed=new NextMessages(restored)
 const taken=resumed.take(first.session,true,"obs2",true,103000)!
 assert.equal(taken.token,first.token);assert.equal(taken.request,first.request)
 assert.equal(taken.state,"sending")
 assert.equal(resumed.take(first.session,true,"obs2",true,103001),null,"stale idle cannot flush another turn")
 store.keep(restored.all(),103002)
 assert.equal(store.restore("",103003).find(c=>c.token===first.token)?.state,"unknown","an attempted send never replays on reload")
 assert.equal(resumed.take(first.session,false,"obs3",true,103004),null)
 assert.equal(resumed.take(first.session,true,"obs4",true,104000),null,"a newer idle snapshot cannot overtake an in-flight send")
 restored.accepted(first.token,104001)
 assert.equal(resumed.take(first.session,true,"obs4",true,104002)?.token,second.token)
})

test("queued inputs use the existing card budget and can be cancelled before any write", () => {
 const cards=new PendingSends(),queue=new NextMessages(cards)
 for(let n=0;n<KEPT_CARDS;n++) assert.ok(queue.add("owned:acp:one","message "+n,[],100000+n))
 assert.equal(queue.add("owned:acp:one","overflow",[],101000),null)
 const first=cards.all()[0];cards.dismiss(first.token)
 assert.ok(queue.add("owned:acp:one","replacement",[],101001))
 assert.notEqual(queue.take(first.session,true,"obs",true,102000)?.token,first.token)
})

test("later attempted cards do not evict an unsent queued input at the shared card budget",()=>{
 const cards=new PendingSends(),queue=new NextMessages(cards)
 const queued=queue.add("owned:acp:one","not yet sent",[],100000)!
 for(let n=0;n<KEPT_CARDS;n++) cards.add("other","attempt "+n,[],100001+n)
 let raw="";const store=new Cards({store:{read:()=>raw,write:s=>{raw=s}},turnWords});store.restore("",101000);store.keep(cards.all(),101000)
 assert.ok(store.restore("",101001).some(c=>c.token===queued.token))
})
