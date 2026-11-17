import type {BrowserCall} from "../session/browser-call"
import type {BotBrowserStatus} from "./bot-browser-entry"

const refused=(code:string)=>Object.assign(new Error(code),{code})
/** Exit uses the captured Bot authority and survives its panel's read cancellation. */
export function createBrowserHandback(call:BrowserCall) {
  const writes=new Set<Promise<unknown>>()
  let closing=false,finished=false,active:Promise<void>|null=null
  const status=async()=>{
    const value=await call<BotBrowserStatus>("GET","/v1/browser/status")
    if(!value?.status||!Array.isArray(value.status.tabs)||!value.owner&&value.status.tabs.length)throw refused("pa_invalid")
    return value
  }
  const controlled=(value:BotBrowserStatus)=>value.status.tabs.filter(tab=>tab.conversation===value.owner&&tab.person===true)
  return {
    closing:()=>closing,
    track<T>(method:"GET"|"POST",path:string,perform:()=>Promise<T>):Promise<T>{
      if(closing)return Promise.reject(refused("pa_stale_version"))
      const promise=perform()
      if(method==="POST"&&(path==="/v1/browser/control"||path==="/v1/browser/open")){
        writes.add(promise);void promise.then(()=>writes.delete(promise),()=>writes.delete(promise))
      }
      return promise
    },
    exit():Promise<void>{
      if(finished)return Promise.resolve()
      if(active)return active
      closing=true
      active=(async()=>{
        await Promise.allSettled([...writes])
        const before=await status()
        const tabs=controlled(before)
        for(const tab of tabs)await call("POST","/v1/browser/control",{tab:tab.tab,person:false,...(tab.browser_mode||before.mode?{browser_mode:tab.browser_mode||before.mode}:{})})
        if(tabs.length){const after=await status();if(after.owner!==before.owner)throw refused("pa_stale_version");if(controlled(after).length)throw refused("browser_person_controlling")}
        finished=true
      })().catch(error=>{closing=false;active=null;throw error})
      return active
    },
  }
}
