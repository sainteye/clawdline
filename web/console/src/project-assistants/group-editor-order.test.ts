import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import {orderedGroupMembers} from "./group-editor-order.ts"
const key=(bot_id:string)=>({machine_id:"machine",bot_id}),id=(bot_id:string)=>JSON.stringify(["machine",bot_id])
const existing=[key("bob"),key("alice")],candidates=[key("alice"),key("charlie"),key("bob")]
test("renaming keeps member order when the candidate list has a different order",()=>{
  assert.deepEqual(orderedGroupMembers(existing,[id("bob"),id("alice")],candidates),existing)
})
test("new members append without changing retained members",()=>{
  assert.deepEqual(orderedGroupMembers(existing,[id("charlie"),id("alice"),id("bob")],candidates),[...existing,key("charlie")])
})
test("removing the new member returns the original order",()=>{
  assert.deepEqual(orderedGroupMembers([...existing,key("charlie")],[id("alice"),id("bob")],candidates),existing)
})
test("deselecting and reselecting an original member is not an order change",()=>{
  assert.deepEqual(orderedGroupMembers(existing,[id("alice"),id("bob")],candidates),existing)
})
test("only currently eligible keys can appear, without duplicates",()=>{
  assert.deepEqual(orderedGroupMembers(existing,[id("alice"),id("charlie"),id("charlie"),id("foreign")],candidates),[key("alice"),key("charlie")])
})
test("a new group follows selection order, while same IDs on other machines are not substitutes",()=>{
  assert.deepEqual(orderedGroupMembers([],[id("charlie"),id("alice")],candidates),[key("charlie"),key("alice")])
  assert.deepEqual(orderedGroupMembers([key("bob")],[id("bob")],[{machine_id:"other",bot_id:"bob"}]),[])
})
