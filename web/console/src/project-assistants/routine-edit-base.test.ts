import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import {routineEditBase} from "./routine-edit-base.ts"

const opened={id:"r",version:3,name:"Check in",instruction:"Say hello",triggers:[{type:"schedule",calendar:"daily"}],timeout_seconds:600,enabled:true}

test("a scheduled fire after the editor opened does not block saving",()=>{
  assert.equal(routineEditBase(opened as never,[{...opened,version:5}] as never),5)
})

test("a definition edited elsewhere keeps the opened revision so the source refuses it",()=>{
  assert.equal(routineEditBase(opened as never,[{...opened,version:5,instruction:"Something else"}] as never),3)
})

test("a new routine and a missing live copy keep their own revision",()=>{
  assert.equal(routineEditBase(null,[]),0)
  assert.equal(routineEditBase(opened as never,[]),3)
})
