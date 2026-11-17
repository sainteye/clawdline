// @ts-expect-error Node's strip-types tests use source extensions.
import {playChime} from "../realtime/chime.ts"
import type * as W from "@clawdline/contract"

const preference="clawdline.bots.completion-sound",changed="clawdline-bot-sound-changed"
let volatilePreference:boolean|undefined
export function botCompletionSoundEnabled():boolean {
  if(volatilePreference!==undefined)return volatilePreference
  try{return localStorage.getItem(preference)!=="off"}catch{return true}
}
export function setBotCompletionSound(enabled:boolean):void {
  volatilePreference=enabled
  try{localStorage.setItem(preference,enabled?"on":"off")}catch{ /* A storage refusal keeps the current browser's default. */ }
  window.dispatchEvent(new Event(changed))
}
export function subscribeBotCompletionSound(listener:()=>void):()=>void {
  const stored=()=>{volatilePreference=undefined;listener()}
  window.addEventListener(changed,listener);window.addEventListener("storage",stored)
  return()=>{window.removeEventListener(changed,listener);window.removeEventListener("storage",stored)}
}

/** Only changes to a known Bot's durable success marker chime; initial snapshots stay quiet. */
export function createCompletionTracker() {
  const sources=new Map<string,Map<string,string>>()
  return {
    retain(scopes:readonly string[]){for(const scope of sources.keys())if(!scopes.includes(scope))sources.delete(scope)},
    observe(scope:string,items:readonly W.ProjectAssistantBotSummary[]):boolean {
      const previous=sources.get(scope),next=new Map<string,string>()
      let completed=false
      for(const item of items){
        if(item.bot.hidden)continue
        const key=JSON.stringify([item.bot.key.bot_id,item.bot.conversation_id]),marker=item.completed_turn_id??""
        if(!item.source.fresh||item.source.state!=="online"||!item.source.readable){if(previous?.has(key))next.set(key,previous.get(key)!);continue}
        if(item.bot.notify!=="off"&&marker&&previous?.has(key)&&previous.get(key)!==marker)completed=true
        next.set(key,marker)
      }
      sources.set(scope,next)
      return completed
    },
  }
}

/** Unlock audio in a real gesture; respect the browser's audio/ringer policy. */
export function startBotCompletionSound(target:Document=document) {
  let audio:AudioContext|null=null,stopped=false
  const unlock=()=>{
    if(stopped||!botCompletionSoundEnabled())return
    try {
      const scope=globalThis as {AudioContext?:typeof AudioContext;webkitAudioContext?:typeof AudioContext}
      const Class=scope.AudioContext??scope.webkitAudioContext
      if(!audio&&Class)audio=new Class()
      if(audio?.state==="suspended")void audio.resume().catch(()=>undefined)
    }catch{ /* refusal-ok: unavailable browser audio leaves push and the visible reply intact. */ }
  }
  target.addEventListener("pointerdown",unlock,true);target.addEventListener("keydown",unlock,true)
  const unsubscribe=subscribeBotCompletionSound(unlock)
  return {
    play(){if(botCompletionSoundEnabled()&&audio?.state==="running"&&target.visibilityState!=="hidden")playChime(audio)},
    stop(){stopped=true;target.removeEventListener("pointerdown",unlock,true);target.removeEventListener("keydown",unlock,true);unsubscribe();if(audio)void audio.close().catch(()=>undefined);audio=null},
  }
}
