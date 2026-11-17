import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import {createCompletionTracker,startBotCompletionSound,setBotCompletionSound,botCompletionSoundEnabled} from "./completion-sound.ts"

const row=(marker="",extra:Partial<W.ProjectAssistantBotSummary>={}):W.ProjectAssistantBotSummary=>({
  bot:{key:{machine_id:"machine",bot_id:"bot"},conversation_id:"chat"},source:{fresh:true,state:"online",readable:true},completed_turn_id:marker,...extra,
} as W.ProjectAssistantBotSummary)

test("completion sound observes only new durable successes and never an initial snapshot or replay",()=>{
  const tracker=createCompletionTracker()
  assert.equal(tracker.observe("account/grant",[row("old")]),false)
  assert.equal(tracker.observe("account/grant",[row("next")]),true)
  assert.equal(tracker.observe("account/grant",[row("next")]),false)
  assert.equal(tracker.observe("account/grant",[row("next",{status:"error"})]),false)
  assert.equal(tracker.observe("account/grant",[row("next",{status:"idle"})]),false)
  assert.equal(tracker.observe("account/grant",[row("late",{source:{...row().source,fresh:false}})]),false)
  assert.equal(tracker.observe("account/grant",[row("late")]),true)
  assert.equal(tracker.observe("new-account/grant",[row("late")]),false)
  tracker.retain(["new-account/grant"])
  assert.equal(tracker.observe("account/grant",[row("later")]),false)
  assert.equal(tracker.observe("new-account/grant",[row("later",{bot:{...row().bot,hidden:true}})]),false)
  assert.equal(tracker.observe("new-account/grant",[row("later")]),false,"unhiding is a new observation, not a completion")
  assert.equal(tracker.observe("new-account/grant",[row("other",{bot:{...row().bot,conversation_id:"other-chat"}})]),false)
})

test("browser audio unlocks in a gesture, honors mute even when storage fails, and closes on teardown",t=>{
  t.mock.timers.enable({apis:["setTimeout"]})
  const keys=["window","localStorage","AudioContext"] as const
  const before=keys.map(key=>Object.getOwnPropertyDescriptor(globalThis,key))
  const win=new EventTarget(),doc=Object.assign(new EventTarget(),{visibilityState:"visible"})
  let made=0,tones=0,closed=0
  class Audio {
    state="running";currentTime=0;destination={}
    constructor(){made++}
    createGain(){return {connect(){},disconnect(){},gain:{setValueAtTime(){},linearRampToValueAtTime(){}}}}
    createOscillator(){return {frequency:{value:0},connect(){},start(){tones++},stop(){}}}
    close(){closed++;return Promise.resolve()}
  }
  Object.defineProperty(globalThis,"window",{configurable:true,value:win})
  Object.defineProperty(globalThis,"localStorage",{configurable:true,value:{getItem:()=>null,setItem:()=>{throw new Error("storage refused")}}})
  Object.defineProperty(globalThis,"AudioContext",{configurable:true,value:Audio})
  try {
    setBotCompletionSound(true)
    const sound=startBotCompletionSound(doc as unknown as Document)
    sound.play();assert.equal(made,0);assert.equal(tones,0)
    doc.dispatchEvent(new Event("pointerdown"));assert.equal(made,1)
    sound.play();assert.equal(tones,2)
    setBotCompletionSound(false);assert.equal(botCompletionSoundEnabled(),false)
    sound.play();assert.equal(tones,2)
    setBotCompletionSound(true);doc.visibilityState="hidden";sound.play();assert.equal(tones,2)
    doc.visibilityState="visible";sound.play();assert.equal(tones,4)
    sound.stop();assert.equal(closed,1)
    doc.dispatchEvent(new Event("pointerdown"));assert.equal(made,1)
  }finally{keys.forEach((key,i)=>{if(before[i])Object.defineProperty(globalThis,key,before[i]!);else Reflect.deleteProperty(globalThis,key)})}
})
