// Motion timing adapted from Codync 9f67eb09084db61b3cca9c84a11bc03df9de29b7.
// Copyright 2026 Po Kai Lee and the Codync contributors. Apache-2.0.
// See third_party/codync/{LICENSE,NOTICE}.
import {useEffect,useRef} from "react"
import type {ProjectAssistantAppearance} from "@clawdline/contract"
import {palette,avatarPaths} from "./avatar-paths"
import type {Words} from "./words"
import {useReduceMotion} from "./codync-rendering"

type Mood = "idle" | "working" | "needsInput" | "failed"

/** Keep the approved BR-06 outline and eyes. Codync supplies the activity
 * states and motion cadence; animation changes only the existing line art. */
export function BotAvatar({appearance,size=44,mood="idle"}:{appearance:ProjectAssistantAppearance;size?:number;mood?:Mood}) {
  const path=avatarPaths[appearance.shape]??avatarPaths.blob
  const color=palette.find(c=>c===appearance.color.toLowerCase())??palette[0]
  const svg=useRef<SVGSVGElement>(null),outline=useRef<SVGPathElement>(null),eyes=useRef<SVGPathElement>(null)
  const reduce=useReduceMotion()

  useEffect(()=>{
    const el=svg.current,body=outline.current,face=eyes.current
    if(!el||!body||!face)return
    face.removeAttribute("transform");body.removeAttribute("opacity")
    if(reduce||(mood!=="working"&&mood!=="needsInput"))return
    let frame=0,drawn=-Infinity,visible=true
    const draw=(time:number)=>{
      frame=0
      if(time-drawn>=33){
        drawn=time
        const t=(time/1000)%3600
        // Upstream's stepped 3.2-second glance and 4.7-second blink, mapped
        // to the original 100-unit outline instead of changing its geometry.
        const glance=mood==="working"?Math.round(Math.sin(t*2*Math.PI/3.2)*1.4)*100/9:0
        const blink=(t/4.7)%1<0.035
        face.setAttribute("transform",`translate(${glance} 47.5) scale(1 ${blink?0.15:1}) translate(0 -47.5)`)
        // The waiting ripple's 5 rad/s cadence becomes an ink pulse along
        // the existing contour. Working retains the slower 1.4 rad/s light.
        const ink=mood==="needsInput"?0.6+0.4*(0.5+0.5*Math.sin(-t*5)):0.88+0.12*(0.5+0.5*Math.cos(t*1.4))
        body.setAttribute("opacity",String(ink))
      }
      if(visible&&!document.hidden)frame=requestAnimationFrame(draw)
    }
    const resume=()=>{
      if(!visible||document.hidden){cancelAnimationFrame(frame);frame=0}
      else if(!frame)frame=requestAnimationFrame(draw)
    }
    draw(performance.now())
    const onScreen=new IntersectionObserver(([entry])=>{visible=entry!.isIntersecting;resume()})
    onScreen.observe(el)
    document.addEventListener("visibilitychange",resume)
    return()=>{onScreen.disconnect();document.removeEventListener("visibilitychange",resume);cancelAnimationFrame(frame)}
  },[path,color,mood,reduce])

  return <svg ref={svg} className="pa-avatar" data-mood={mood} width={size} height={size} viewBox="-5 -5 110 110" aria-hidden="true" focusable="false">
    <path ref={outline} d={path} fill="none" stroke={color} strokeWidth="4.4" strokeLinejoin="round"/>
    <path ref={eyes} d="M36 40 V55 M63 40 V55" fill="none" stroke={color} strokeWidth="5.5" strokeLinecap="round"/>
  </svg>
}
export function AppearancePicker({value,onChange,w}: {value: ProjectAssistantAppearance; onChange:(value:ProjectAssistantAppearance)=>void; w:Words}) {
  return <fieldset className="pa-appearance"><legend>{w.appearance}</legend>
    <div className="pa-shapes">{Object.keys(avatarPaths).map(shape=><button type="button" key={shape} aria-label={w[`shape_${shape}` as keyof Words]} aria-pressed={value.shape===shape} onClick={()=>onChange({...value,shape:shape as ProjectAssistantAppearance["shape"]})}><BotAvatar appearance={{...value,shape:shape as ProjectAssistantAppearance["shape"]}} size={40}/></button>)}</div>
    <div className="pa-colors">{palette.map(color=><button type="button" key={color} aria-label={color} aria-pressed={value.color===color} style={{color}} onClick={()=>onChange({...value,color})}>●</button>)}</div>
  </fieldset>
}
