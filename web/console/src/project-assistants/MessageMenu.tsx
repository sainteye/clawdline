import {createPortal} from "react-dom"
import {useLayoutEffect,useRef,useState,type CSSProperties,type ReactNode,type RefObject} from "react"
import {afterSurfaceExit} from "./surface-motion"

/** An anchored message menu never participates in the conversation's measured height. */
export function MessageMenu({open,anchor,label,onClose,children}:{open:boolean;anchor:RefObject<HTMLButtonElement|null>;label:string;onClose:(focus:boolean)=>void;children:ReactNode}){
 const panel=useRef<HTMLDivElement>(null),close=useRef(onClose);close.current=onClose
 const [position,setPosition]=useState<CSSProperties>({position:"fixed",visibility:"hidden"})
 const [present,setPresent]=useState(false)
 useLayoutEffect(()=>{
  if(open){setPresent(true);return}
  if(present&&panel.current)return afterSurfaceExit(panel.current,()=>setPresent(false))
 },[open,present])
 useLayoutEffect(()=>{
  if(!open)return
  const place=()=>{
   const button=anchor.current,node=panel.current;if(!button||!node)return
   const a=button.getBoundingClientRect(),width=node.offsetWidth,height=node.offsetHeight,v=window.visualViewport,x=v?.offsetLeft??0,y=v?.offsetTop??0,w=v?.width??innerWidth,h=v?.height??innerHeight
   const below=a.bottom+6,above=a.top-height-6
   setPosition({position:"fixed",visibility:"visible",left:Math.max(x+8,Math.min(a.right-width,x+w-width-8)),top:Math.max(y+8,Math.min(below+height<=y+h-8?below:above,y+h-height-8)),maxHeight:h-16})
  }
  place()
  const frame=requestAnimationFrame(()=>panel.current?.querySelector<HTMLElement>('button:not(:disabled)')?.focus({preventScroll:true}))
  const away=(e:PointerEvent)=>{if(!panel.current?.contains(e.target as Node)&&!anchor.current?.contains(e.target as Node))close.current(false)}
  const keys=(e:KeyboardEvent)=>{
   if(e.isComposing||e.defaultPrevented)return
   if(e.key==="Escape"){e.preventDefault();e.stopPropagation();close.current(true)}
   else if(e.key==="Tab")close.current(true)
   else if(["ArrowDown","ArrowUp","ArrowLeft","ArrowRight","Home","End"].includes(e.key)){
    const choices=[...panel.current!.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')],at=choices.indexOf(document.activeElement as HTMLButtonElement)
    e.preventDefault();const next=e.key==="Home"?0:e.key==="End"?choices.length-1:(at+(["ArrowUp","ArrowLeft"].includes(e.key)?-1:1)+choices.length)%choices.length;choices[next]?.focus({preventScroll:true})
   }
  }
  document.addEventListener("pointerdown",away,true);document.addEventListener("keydown",keys,true);window.addEventListener("resize",place);window.addEventListener("scroll",place,true);window.visualViewport?.addEventListener("resize",place);window.visualViewport?.addEventListener("scroll",place)
  const observer=new ResizeObserver(place);if(panel.current)observer.observe(panel.current)
  return()=>{cancelAnimationFrame(frame);observer.disconnect();document.removeEventListener("pointerdown",away,true);document.removeEventListener("keydown",keys,true);window.removeEventListener("resize",place);window.removeEventListener("scroll",place,true);window.visualViewport?.removeEventListener("resize",place);window.visualViewport?.removeEventListener("scroll",place)}
 },[open,anchor])
 return open||present?createPortal(<div className="project-assistants pa-message-menu-root"><div ref={panel} className="pa-message-actions pa-message-menu" role="menu" aria-label={label} inert={!open} data-closing={!open||undefined} style={position}>{children}</div></div>,document.body):null
}
