import {useSyncExternalStore} from "react"

/** One switch for the dispatched-Session count, the waiting-on-work state and
 * the chat strip: they are one fact shown three ways. On unless turned off. */
const preference="clawdline.bots.show-dispatched",changed="clawdline-bot-dispatched-changed"
let volatilePreference:boolean|undefined
export function dispatchedWorkShown():boolean {
  if(volatilePreference!==undefined)return volatilePreference
  try{return localStorage.getItem(preference)!=="off"}catch{return true}
}
export function setDispatchedWorkShown(shown:boolean):void {
  volatilePreference=shown
  try{localStorage.setItem(preference,shown?"on":"off")}catch{ /* A storage refusal keeps this page's choice only. */ }
  window.dispatchEvent(new Event(changed))
}
function subscribe(listener:()=>void):()=>void {
  const stored=()=>{volatilePreference=undefined;listener()}
  window.addEventListener(changed,listener);window.addEventListener("storage",stored)
  return()=>{window.removeEventListener(changed,listener);window.removeEventListener("storage",stored)}
}
export function useDispatchedWorkShown():boolean {
  return useSyncExternalStore(subscribe,dispatchedWorkShown,()=>true)
}
