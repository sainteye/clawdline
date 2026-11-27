import {useEffect,type RefObject} from "react"

/**
 * Scrolls the chat to the message a push about it opened, once the page is
 * loaded, and consumes the target. A message outside the loaded page leaves
 * the chat where it opened.
 */
export function useMessageFocus(id:string|undefined,loaded:boolean,scroll:RefObject<HTMLDivElement|null>,following:{current:boolean},setFollowing:(on:boolean)=>void,done?:()=>void){
  useEffect(()=>{
    if(!id||!loaded)return
    const frame=requestAnimationFrame(()=>{
      const viewport=scroll.current,target=viewport?.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(id)}"]`)
      done?.()
      if(!viewport||!target)return
      following.current=false;setFollowing(false)
      revealInChat(viewport,target)
    })
    return()=>cancelAnimationFrame(frame)
  },[id,loaded])
}

/** Brings one chat row into view and focus, centred unless taller than the viewport. */
export function revealInChat(viewport:HTMLElement,target:HTMLElement){
  const still=window.matchMedia?.("(prefers-reduced-motion: reduce)").matches
  target.scrollIntoView({block:target.offsetHeight>=viewport.clientHeight?"start":"center",behavior:still?"auto":"smooth"})
  target.focus({preventScroll:true})
}
