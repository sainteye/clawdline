import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import {emptyRosterState, getRosterState, saveRosterState, pinRosterBot, moveRosterBot, reorderKeys, rosterStorageKey} from "./roster-state.ts"
const store=()=>{const values=new Map<string,string>();return {getItem:(key:string)=>values.get(key)??null,setItem:(key:string,value:string)=>{values.set(key,value)}}}
test("pinning and custom order survive reload, separated by authenticated account and machine/Bot identity",()=>{
  const storage=store(), a=JSON.stringify(["one","same"]), b=JSON.stringify(["two","same"])
  let state=pinRosterBot(emptyRosterState(),a,true)
  state=moveRosterBot(state,b,a,true,[a,b])
  state={...state,order:"unread",projectSortOrder:[JSON.stringify(["two","p"]),JSON.stringify(["one","p"])]}
  assert.equal(saveRosterState("account-a",state,storage),true)
  assert.deepEqual(getRosterState("account-a",storage),state)
  assert.deepEqual(getRosterState("account-b",storage),emptyRosterState())
  assert.equal(saveRosterState("",state,storage),false)
  assert.deepEqual(state.botSortOrder,[b,a])
  assert.deepEqual(state.pinnedBotKeys,[a,b])
})
test("filtered moves retain unseen preferences and pin/unpin only the dragged Bot",()=>{
  const state={pinnedBotKeys:["hidden","a"],projectSortOrder:["project"],botSortOrder:["hidden","a","b"]}
  const next=moveRosterBot(state,"a","b",false,["a","b"])
  assert.deepEqual(next.botSortOrder,["hidden","b","a"])
  assert.deepEqual(next.pinnedBotKeys,["hidden"])
  assert.deepEqual(next.projectSortOrder,["project"])
  assert.equal(moveRosterBot(state,"foreign","b",true,["a","b"]),state)
  assert.deepEqual(reorderKeys(["a","b"],"foreign","b"),["a","b"])
  assert.deepEqual(state.botSortOrder,["hidden","a","b"])
})
test("bad or denied storage cannot masquerade as saved preferences",()=>{
  const storage=store()
  storage.setItem(rosterStorageKey("account"),'{"pinnedBotKeys":[42]}')
  assert.throws(()=>getRosterState("account",storage))
  assert.equal(saveRosterState("account",emptyRosterState(),{getItem:()=>null,setItem:()=>{throw Error("quota")}}),false)
  assert.deepEqual(getRosterState("account"),emptyRosterState())
})
