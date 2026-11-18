import {createPortal} from "react-dom"
import {useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode} from "react"

export interface PaMenuItem { id:string; label:string; onSelect:()=>void; disabled?:boolean }

/** An anchored action menu shared by the roster and chat header. */
export function PaMenu({label,items,floating=false,triggerIcon}:{label:string;items:readonly PaMenuItem[];floating?:boolean;triggerIcon?:ReactNode}) {
  const [open,setOpen]=useState(false),[active,setActive]=useState(0)
  const root=useRef<HTMLDivElement>(null),button=useRef<HTMLButtonElement>(null),menu=useRef<HTMLDivElement>(null)
  const [floatingStyle,setFloatingStyle]=useState<CSSProperties>({position:"fixed",visibility:"hidden"})
  const enabled=items.map((it,i)=>it.disabled?-1:i).filter(i=>i>=0)
  const close=(focus=true)=>{setOpen(false);if(focus)button.current?.focus({preventScroll:true})}
  useEffect(()=>{
    if(!open)return
    const focus=()=>menu.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.focus({preventScroll:true})
    if(!floating){focus();return}
    // The portal is measured while hidden; focus only after its position is visible.
    const frame=requestAnimationFrame(focus)
    return()=>cancelAnimationFrame(frame)
  },[open,active,floating])
  useEffect(()=>{
    if(!open)return
    const away=(event:PointerEvent)=>{if(!root.current?.contains(event.target as Node)&&!menu.current?.contains(event.target as Node))close(false)}
    const keys=(event:KeyboardEvent)=>{
      const at=enabled.indexOf(active)
      if(event.key==="Escape"){event.preventDefault();event.stopPropagation();close();return}
      if(event.key==="Tab"){close(false);return}
      if(event.key==="ArrowDown"){event.preventDefault();setActive(enabled[Math.min(enabled.length-1,at+1)]??active)}
      else if(event.key==="ArrowUp"){event.preventDefault();setActive(enabled[Math.max(0,at-1)]??active)}
      else if(event.key==="Home"){event.preventDefault();setActive(enabled[0]??active)}
      else if(event.key==="End"){event.preventDefault();setActive(enabled.at(-1)??active)}
    }
    const node=menu.current
    document.addEventListener("pointerdown",away,true);node?.addEventListener("keydown",keys)
    return()=>{document.removeEventListener("pointerdown",away,true);node?.removeEventListener("keydown",keys)}
  },[open,active,items])
  useLayoutEffect(()=>{
    if(!open||!floating)return
    const place=()=>{
      if(!button.current||!menu.current)return
      const anchor=button.current.getBoundingClientRect(),box=menu.current.getBoundingClientRect(),viewport=window.visualViewport
      const x=viewport?.offsetLeft??0,y=viewport?.offsetTop??0,width=viewport?.width??innerWidth,height=viewport?.height??innerHeight
      const below=anchor.bottom+6,above=anchor.top-box.height-6
      const top=Math.max(y+8,Math.min(below+box.height<=y+height-8?below:above,y+height-box.height-8))
      const left=Math.max(x+8,Math.min(anchor.right-box.width,x+width-box.width-8))
      setFloatingStyle({position:"fixed",top,left,right:"auto",zIndex:30,maxHeight:height-16,overflowY:"auto"})
    }
    place();window.addEventListener("resize",place);window.addEventListener("scroll",place,true);window.visualViewport?.addEventListener("resize",place);window.visualViewport?.addEventListener("scroll",place)
    return()=>{window.removeEventListener("resize",place);window.removeEventListener("scroll",place,true);window.visualViewport?.removeEventListener("resize",place);window.visualViewport?.removeEventListener("scroll",place)}
  },[open,floating])
  const choices=<div className="pa-menu-list" role="menu" aria-label={label} ref={menu} style={floating?floatingStyle:undefined}>
    {items.map((it,i)=><button key={it.id} type="button" role="menuitem" data-index={i} tabIndex={i===active?0:-1} disabled={it.disabled}
      onClick={()=>{close();it.onSelect()}}>{it.label}</button>)}
  </div>
  return <div className="pa-menu" ref={root} data-open={open||undefined}>
    <button type="button" ref={button} className="pa-circle" aria-label={label} title={label} aria-haspopup="menu" aria-expanded={open}
      onClick={()=>{if(open)close();else{setActive(enabled[0]??0);setOpen(true)}}}
      onKeyDown={e=>{if(e.key==="ArrowDown"){e.preventDefault();setActive(enabled[0]??0);setOpen(true)}}}>
      {triggerIcon??<svg viewBox="0 0 16 16" width="16" height="16" fill="currentColor" aria-hidden="true"><circle cx="3" cy="8" r="1.4"/><circle cx="8" cy="8" r="1.4"/><circle cx="13" cy="8" r="1.4"/></svg>}
    </button>
    {open&&(floating?createPortal(<div className="project-assistants">{choices}</div>,document.body):choices)}
  </div>
}
