import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { botsSwitchShown } from "./visibility.ts"
test("a release build hides the Bots switch while Bots is off",()=>{
  for (const version of ["v2.20.0","v2.20.0-rc.1","","unknown"]) assert.equal(botsSwitchShown(version,false),false,version)
})
test("a dev build shows the switch, and Bots already on can always be turned off",()=>{
  assert.equal(botsSwitchShown("v2.19.0-dev.9c1a8d3",false),true)
  assert.equal(botsSwitchShown("v2.20.0",true),true)
})
