import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { composerEnter } from "./composer-keyboard.ts"
test("Enter sends while Shift and Alt Enter insert a newline",()=>{
  assert.equal(composerEnter({key:"Enter"}),"send")
  assert.equal(composerEnter({key:"Enter",shiftKey:true}),"newline")
  assert.equal(composerEnter({key:"Enter",altKey:true}),"newline")
})
test("composition confirmation and held Enter never admit a send",()=>{
  for(const event of [{key:"Enter",isComposing:true},{key:"Enter",keyCode:229},{key:"Enter",repeat:true},{key:"a"}])assert.equal(composerEnter(event),"ignore")
  assert.equal(composerEnter({key:"Enter"},true),"ignore")
  assert.equal(composerEnter({key:"Enter"},false),"send")
})
