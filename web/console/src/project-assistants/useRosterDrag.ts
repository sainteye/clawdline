import { useEffect, useLayoutEffect, useRef, type RefObject } from "react"
import type { RosterDrag } from "./RosterHandle"

// Gesture recognition and motion timings, not storage or queue limits.
export const RosterHoldMs = 350
const pressSlop = 8, slideMs = 180, edge = 36, edgeSpeed = 12
const entrySelector = ".pa-roster-entry"
const projectSelector = ".pa-project-group[data-roster-project]"
const rowSelector = `${entrySelector},${projectSelector}`
type Point = {x:number;y:number}
type Callbacks = {start:(source:RosterDrag,direct:boolean)=>void;over:(target:RosterDrag)=>void;finish:(commit:boolean)=>void}

/** A pending press allows native scrolling. Only a completed lift owns the gesture. */
export function useRosterDrag(root:RefObject<HTMLElement|null>, callbacks:Callbacks) {
  const latest=useRef(callbacks);latest.current=callbacks
  const cancel=useRef<()=>void>(()=>{})
  const positions=useRef(new Map<HTMLElement,number>())
  const slides=useRef(new Map<HTMLElement,Animation>())
  useLayoutEffect(()=>{
    const node=root.current
    const next=new Map<HTMLElement,number>()
    const scrollTop=node?.querySelector(".pa-roster-scroll")?.scrollTop??0
    const kind=node?.dataset.rosterDragging
    const reduce=matchMedia("(prefers-reduced-motion: reduce)").matches
    // Measure resting positions before animating either the sections or their children.
    for(const animation of slides.current.values())animation.cancel()
    slides.current.clear()
    for(const row of node?.querySelectorAll<HTMLElement>(rowSelector)??[])next.set(row,row.getBoundingClientRect().top+scrollTop)
    for(const [row,top] of next){
      const old=positions.current.get(row)
      const moving=kind&&(kind==="project"?row.matches(projectSelector):row.matches(entrySelector))
      if(moving&&old!==undefined&&old!==top&&!reduce&&row.dataset.dragging!=="true"){
        slides.current.set(row,row.animate([{transform:`translateY(${old-top}px)`},{transform:"translateY(0)"}],{duration:slideMs,easing:"cubic-bezier(.2,.8,.2,1)"}))
      }
    }
    positions.current=next
  })
  useEffect(()=>{
    const node=root.current;if(!node)return
    let gesture:{id:number;touch:boolean;from:Point;at:Point;row:HTMLElement;source:RosterDrag;lifted:boolean;ghost:HTMLElement|null;grab:Point;direct:boolean}|null=null
    let timer:ReturnType<typeof setTimeout>|undefined,frame=0,swallow=false
    const target=(element:Element|null):RosterDrag|null=>{
      const row=element?.closest<HTMLElement>("[data-roster-drop-kind]")
      if(!row||!node.contains(row))return null
      const kind=row.dataset.rosterDropKind,key=row.dataset.rosterDropKey
      return key&&(kind==="bot"||kind==="project")?{kind,key}:null
    }
    const suppress=(event:Event)=>{if(swallow){event.preventDefault();event.stopImmediatePropagation();swallow=false}}
    const cleanup=()=>{
      clearTimeout(timer);cancelAnimationFrame(frame);frame=0
      gesture?.ghost?.remove();node.removeAttribute("data-roster-dragging")
      document.documentElement.classList.remove("pa-roster-dragging")
    }
    const finish=(commit:boolean)=>{
      const lifted=gesture?.lifted;cleanup();gesture=null
      if(lifted){latest.current.finish(commit);swallow=true}
    }
    cancel.current=()=>finish(false)
    const update=(vertical=0)=>{
      const g=gesture;if(!g?.lifted)return
      if(!node.contains(g.row)){finish(false);return}
      g.ghost!.style.translate=`${g.at.x-g.grab.x}px ${g.at.y-g.grab.y}px`
      const under=document.elementFromPoint(g.at.x,g.at.y)
      const project=g.source.kind==="project"
      const row=under?.closest<HTMLElement>(project?projectSelector:"[data-roster-drop-kind]")
      // A neighbour still sliding out of the way is not a new target.
      if(!row||slides.current.get(row)?.playState==="running")return
      if(project){
        const position=g.row.compareDocumentPosition(row)
        // Blocks of unequal height can stay under the pointer after a move.
        // Only move past a section in the direction the pointer or scroll is travelling.
        const direction=vertical>0?Node.DOCUMENT_POSITION_FOLLOWING:Node.DOCUMENT_POSITION_PRECEDING
        if(!vertical||!(position&direction))return
      }
      const over=target(row);if(over)latest.current.over(over)
    }
    const tick=()=>{
      const g=gesture,scroller=node.querySelector<HTMLElement>(".pa-roster-scroll")
      if(!g?.lifted||!scroller)return
      if(!node.contains(g.row)){finish(false);return}
      const box=scroller.getBoundingClientRect()
      if(g.at.x>=box.left&&g.at.x<=box.right){
        const reach=Math.max(0,box.top+edge-g.at.y)-Math.max(0,g.at.y-(box.bottom-edge))
        if(reach){
          const previous=scroller.scrollTop
          scroller.scrollTop-=Math.sign(reach)*Math.min(edgeSpeed,Math.abs(reach)/edge*edgeSpeed)
          update(scroller.scrollTop-previous)
        }
      }
      frame=requestAnimationFrame(tick)
    }
    const lift=()=>{
      const g=gesture;if(!g||!node.contains(g.row))return finish(false)
      const box=g.row.getBoundingClientRect();g.lifted=true
      g.grab={x:g.from.x-box.left,y:g.from.y-box.top}
      const ghost=document.createElement("div");ghost.className="project-assistants pa-roster-lift";ghost.setAttribute("aria-hidden","true");ghost.inert=true
      const copy=g.row.cloneNode(true) as HTMLElement
      copy.classList.remove("pa-roster-entry");copy.classList.add("pa-roster-ghost-row")
      for(const el of copy.querySelectorAll("[data-bot-key],[data-group-key]")){el.removeAttribute("data-bot-key");el.removeAttribute("data-group-key")}
      for(const el of [copy,...copy.querySelectorAll<HTMLElement>("[id],[data-roster-drop-kind]")]){el.removeAttribute("id");el.removeAttribute("data-roster-drop-kind");el.removeAttribute("data-roster-drop-key")}
      for(const el of copy.querySelectorAll(".pa-roster-actions,.pa-roster-read"))el.remove()
      ghost.append(copy);ghost.style.width=`${box.width}px`;document.body.append(ghost);g.ghost=ghost
      node.dataset.rosterDragging=g.source.kind;document.documentElement.classList.add("pa-roster-dragging")
      latest.current.start(g.source,g.direct);update();frame=requestAnimationFrame(tick)
    }
    const begin=(element:Element|null,id:number,touch:boolean,at:Point)=>{
      if(gesture){finish(false);return}
      swallow=false
      const handle=element?.closest(".pa-roster-handle")
      const heading=element?.closest(".pa-project-heading")
      const row=heading?heading.closest<HTMLElement>(projectSelector):element?.closest<HTMLElement>(entrySelector)
      if(!row||!node.contains(row)||(!handle&&element?.closest(".pa-roster-actions,.pa-roster-read")))return
      const source=target(row);if(!source)return
      gesture={id,touch,from:at,at,row,source,lifted:false,ghost:null,grab:{x:0,y:0},direct:!handle}
      if(handle)lift();else timer=setTimeout(lift,RosterHoldMs)
    }
    const move=(at:Point,event:Event)=>{
      const g=gesture;if(!g)return
      if(!g.lifted&&Math.hypot(at.x-g.from.x,at.y-g.from.y)>pressSlop){finish(false);return}
      const vertical=at.y-g.at.y
      g.at=at
      if(g.lifted){if(event.cancelable)event.preventDefault();update(vertical)}
    }
    const end=()=>{
      const g=gesture;if(!g)return
      const box=node.getBoundingClientRect(),inside=g.at.x>=box.left&&g.at.x<=box.right&&g.at.y>=box.top&&g.at.y<=box.bottom
      finish(inside)
    }
    const down=(e:PointerEvent)=>{if(e.pointerType!=="touch"&&e.button===0&&!e.ctrlKey&&!e.metaKey&&!e.shiftKey)begin(e.target as Element,e.pointerId,false,{x:e.clientX,y:e.clientY})}
    const pointerMove=(e:PointerEvent)=>{if(gesture&&!gesture.touch&&gesture.id===e.pointerId)move({x:e.clientX,y:e.clientY},e)}
    const pointerUp=(e:PointerEvent)=>{if(gesture&&!gesture.touch&&gesture.id===e.pointerId)end()}
    const pointerCancel=(e:PointerEvent)=>{if(gesture&&!gesture.touch&&gesture.id===e.pointerId)finish(false)}
    const touchStart=(e:TouchEvent)=>{if(e.touches.length!==1){finish(false);return}const t=e.changedTouches[0];begin(e.target as Element,t.identifier,true,{x:t.clientX,y:t.clientY})}
    const touchMove=(e:TouchEvent)=>{if(!gesture?.touch)return;const t=[...e.touches].find(t=>t.identifier===gesture?.id);if(t)move({x:t.clientX,y:t.clientY},e)}
    const touchEnd=(e:TouchEvent)=>{if(gesture?.touch&&[...e.changedTouches].some(t=>t.identifier===gesture?.id))end()}
    const abort=()=>finish(false)
    const key=(e:KeyboardEvent)=>{if(e.key==="Escape"&&gesture){if(gesture.lifted){e.preventDefault();e.stopPropagation()}finish(false)}}
    const context=(e:Event)=>{if(gesture){e.preventDefault();e.stopPropagation();if(!gesture.lifted)lift()}}
    // Native non-passive touchmove allows scrolling until the hold is recognized.
    node.addEventListener("pointerdown",down);node.addEventListener("touchstart",touchStart,{passive:true})
    node.addEventListener("contextmenu",context);node.addEventListener("click",suppress,true)
    window.addEventListener("pointermove",pointerMove);window.addEventListener("pointerup",pointerUp);window.addEventListener("pointercancel",pointerCancel)
    window.addEventListener("touchmove",touchMove,{passive:false});window.addEventListener("touchend",touchEnd);window.addEventListener("touchcancel",abort)
    window.addEventListener("keydown",key,true);window.addEventListener("blur",abort)
    return ()=>{
      cleanup();gesture=null
      node.removeEventListener("pointerdown",down);node.removeEventListener("touchstart",touchStart);node.removeEventListener("contextmenu",context);node.removeEventListener("click",suppress,true)
      window.removeEventListener("pointermove",pointerMove);window.removeEventListener("pointerup",pointerUp);window.removeEventListener("pointercancel",pointerCancel)
      window.removeEventListener("touchmove",touchMove);window.removeEventListener("touchend",touchEnd);window.removeEventListener("touchcancel",abort);window.removeEventListener("keydown",key,true);window.removeEventListener("blur",abort)
    }
  },[root])
  return {cancel:()=>cancel.current()}
}
